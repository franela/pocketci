package main

import (
	"context"
	"flag"
	"log"
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
		log.Fatalf("failed to connect to dagger: %v", err)
	}
	defer client.Close()

	server, err := pocketci.NewServer(client, pocketci.ServerOptions{
		GithubUsername: os.Getenv("GITHUB_USERNAME"),
		GithubPassword: os.Getenv("GITHUB_TOKEN"),
	})
	if err != nil {
		log.Fatalf("failed to create pocketci server: %s", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/", server)
	srv := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	log.Println("starting proxy server in port 8080")
	if err = srv.ListenAndServe(); err != nil {
		log.Printf("serve exited with: %v", err)
	}
}
