package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"

	"dagger.io/dagger"
	"github.com/franela/pocketci/pocketci"
)

func main() {
	flag.Parse()

	ctx := context.Background()
	client, err := dagger.Connect(ctx, dagger.WithLogOutput(os.Stderr))
	if err != nil {
		slog.Error("failed to connect to dagger", slog.String("error", err.Error()))
	}
	defer client.Close()

	server, err := pocketci.NewServer(client, pocketci.ServerOptions{
		GithubUsername:  os.Getenv("GITHUB_USERNAME"),
		GithubPassword:  os.Getenv("GITHUB_TOKEN"),
		GithubSignature: os.Getenv("X_HUB_SIGNATURE"),
	})
	if err != nil {
		slog.Error("failed to create pocketci server", slog.String("error", err.Error()))
	}

	mux := http.NewServeMux()
	mux.Handle("/", server)
	srv := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	slog.Info("starting pocketci at 8080")
	if err = srv.ListenAndServe(); err != nil {
		slog.Error("server exited", slog.String("error", err.Error()))
	}
}
