package main

import (
	"context"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"time"

	"github.com/davecgh/go-spew/spew"
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

	ticker := time.NewTicker(10 * time.Second)

	// queue := make(chan struct{})

	for {
		select {
		case <-ticker.C:
			repos, err := getRepos(rootCtx, clientSet)
			if err != nil {
				slog.Error("Failed to get repos", "error", err)
			} else {
				spew.Dump(repos)
			}

		case <-rootCtx.Done():
			cancel()
		}
	}

}

type Repository struct {
	Name        string
	Namespace   string
	URL         *url.URL
	AccessToken string
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
		repositories[i] = Repository{
			Name:      repo.GetName(),
			Namespace: repo.GetNamespace(),
		}
	}

	return nil, nil
}
