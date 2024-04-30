package main

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/storage/memory"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"
)

type client struct {
	*dynamic.DynamicClient
}

func main() {
	ctx := context.Background()

	config, err := rest.InClusterConfig()
	if err != nil {
		slog.Error("Failed to initialize in-cluster config", "error", err)
		os.Exit(1)
	}

	clientSet, err := dynamic.NewForConfig(config)
	if err != nil {
		slog.Error("Failed to create client set", "error", err)
		os.Exit(1)
	}

	slog.Info("Ernesto initialized")

	cc := &client{clientSet}

	repos, err := cc.getRepos(ctx)
	if err != nil {
		slog.Error("Failed to get repos", "error", err)
	} else {
		spew.Dump(repos)
	}

	wg := &sync.WaitGroup{}

	for _, repo := range repos {
		wg.Add(1)
		go cc.processRepo(ctx, wg, repo)
	}

	wg.Wait()
}

type Repository struct {
	Name         string
	Namespace    string
	URL          string
	AccessToken  string
	Username     string
	Unstructured unstructured.Unstructured
}

func (c *client) getRepos(ctx context.Context) ([]Repository, error) {
	resource := schema.GroupVersionResource{
		Group:    "0x42.in",
		Version:  "v1alpha1",
		Resource: "githubrepositories",
	}
	repos, err := c.
		Resource(resource).
		Namespace("tacos").
		List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	repositories := make([]Repository, len(repos.Items))
	for i, repo := range repos.Items {
		slog.Info("Found repo", "name", repo.Object["metadata"].(map[string]any)["name"])

		spec := repo.Object["spec"].(map[string]any)

		repositories[i] = Repository{
			Name:         repo.GetName(),
			Namespace:    repo.GetNamespace(),
			URL:          spec["repoUrl"].(string),
			AccessToken:  spec["accessToken"].(string),
			Username:     spec["username"].(string),
			Unstructured: repo,
		}
	}

	return repositories, nil
}

func getLatestCommit(ctx context.Context, repo Repository) (string, error) {
	r, err := git.Clone(memory.NewStorage(), nil, &git.CloneOptions{
		URL: repo.URL,
		Auth: &http.BasicAuth{
			Username: repo.Username,
			Password: repo.AccessToken,
		},
	})

	if err != nil {
		return "", err
	}

	ref, err := r.Head()
	if err != nil {
		return "", err
	}

	return ref.Hash().String(), nil
}

type unstructuredChange struct {
	keys  []string
	value string
}

func (c *client) processRepo(ctx context.Context, wg *sync.WaitGroup, repo Repository) {
	hash, err := getLatestCommit(ctx, repo)
	if err != nil {
		slog.Error("Failed to get latest commit",
			"error", err,
			"repository", repo.URL)
		return
	}

	slog.Info("Latest commit hash retrieved from repository",
		"repository", repo.Name,
		"hash", hash)

	changeset := []unstructuredChange{
		{keys: []string{"metadata", "annotations", "ernesto.0x42.in/last-sync-time"}, value: time.Now().Format(time.RFC1123)},
		{keys: []string{"metadata", "annotations", "ernesto.0x42.in/commit-hash"}, value: hash},
	}

	resource := schema.GroupVersionResource{
		Group:    "0x42.in",
		Version:  "v1alpha1",
		Resource: "githubrepositories",
	}

	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		result, getErr := c.DynamicClient.Resource(resource).
			Namespace(repo.Namespace).
			Get(ctx, repo.Name, metav1.GetOptions{})
		if getErr != nil {
			return getErr
		}

		for _, change := range changeset {
			if err := unstructured.SetNestedField(result.Object, change.value, change.keys...); err != nil {
				return err
			}
		}

		_, updateErr := c.Resource(resource).Namespace(repo.Namespace).Update(ctx, result, metav1.UpdateOptions{})
		return updateErr
	})

	if retryErr != nil {
		slog.Warn("failed to update GithubRepository",
			"error", retryErr.Error(),
			"namespace", repo.Namespace,
			"name", repo.Name,
		)
	}

	defer wg.Done()
}
