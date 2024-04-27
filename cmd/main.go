package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/storage/memory"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

func main() {
	ctx := context.Background()

	rootCtx, cancel := signal.NotifyContext(ctx, os.Interrupt)
	defer cancel()

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

	repos, err := getRepos(rootCtx, clientSet)
	if err != nil {
		slog.Error("Failed to get repos", "error", err)
	} else {
		spew.Dump(repos)
	}

	ticker := time.NewTicker(1 * time.Minute)

	// queue := make(chan struct{})

	wg := &sync.WaitGroup{}
Loop:
	for {
		select {
		case <-ticker.C:
			repos, err := getRepos(rootCtx, clientSet)
			if err != nil {
				slog.Error("Failed to get repos", "error", err)
			} else {
				for _, repo := range repos {
					wg.Add(1)
					go processRepo(ctx, wg, repo)
				}
			}

		case <-rootCtx.Done():
			cancel()
			break Loop
		}
	}
	wg.Wait()
}

type Repository struct {
	Name        string
	Namespace   string
	URL         string
	AccessToken string
	Username    string
}

func getRepos(ctx context.Context, clientSet *dynamic.DynamicClient) ([]Repository, error) {
	resource := schema.GroupVersionResource{
		Group:    "0x42.in",
		Version:  "v1alpha1",
		Resource: "githubrepositories",
	}
	repos, err := clientSet.
		Resource(resource).
		Namespace("tacos").
		List(ctx, v1.ListOptions{})
	if err != nil {
		return nil, err
	}

	repositories := make([]Repository, len(repos.Items))
	for i, repo := range repos.Items {
		slog.Info("Found repo", "name", repo.Object["metadata"].(map[string]any)["name"])

		spec := repo.Object["spec"].(map[string]any)

		repositories[i] = Repository{
			Name:        repo.GetName(),
			Namespace:   repo.GetNamespace(),
			URL:         spec["repoUrl"].(string),
			AccessToken: spec["accessToken"].(string),
			Username:    spec["username"].(string),
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

func processRepo(ctx context.Context, wg *sync.WaitGroup, repo Repository) {
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

	defer wg.Done()
}
