package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"

	"dagger.io/dagger"
)

func main() {
	flag.Parse()

	ctx := context.Background()
	client, err := dagger.Connect(ctx, dagger.WithLogOutput(os.Stderr))
	if err != nil {
		log.Fatalf("failed to connect to dagger: %v", err)
	}

	// we're warming up the webhook container here so we don't make the
	// user wait for the first request to be served
	if _, err = webhookContainer(client).Sync(ctx); err != nil {
		log.Fatalf("failed to build webhook: %v", err)
	}

	http.HandleFunc("/", gitCloneProxy())

	fmt.Println("starting proxy server in port 8080")
	err = http.ListenAndServe(fmt.Sprintf(":%d", 8080), nil)
	if err != nil {
		log.Printf("failed to start server: %v", err)
	}

	defer client.Close()
}

type GithubWebhook struct {
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	After string `json:"after"`
}

// gitCloneProxy returns a handler that will first clone a git repository into
// the specified directory and then proxy the request to the reverse proxy.
func gitCloneProxy() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			log.Printf("failed to get request body: %s", err.Error())
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		r.Body = io.NopCloser(bytes.NewBuffer(b))

		gh := &GithubWebhook{}
		if err = json.Unmarshal(b, gh); err != nil {
			log.Printf("failed to decode JSON payload: %s", err.Error())
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		ctx := r.Context()
		client, err := dagger.Connect(ctx, dagger.WithLogOutput(os.Stderr))
		if err != nil {
			log.Printf("fail to connect to dagger client: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer client.Close()

		repo := client.Git("https://github.com/" + gh.Repository.FullName).Commit(gh.After).Tree()
		fullRepo := strings.Split(gh.Repository.FullName, "/")
		repoName := fullRepo[len(fullRepo)-1]
		svc := webhookContainer(client).
			WithDirectory("/"+repoName, repo).
			WithWorkdir("/"+repoName).
			WithExposedPort(9000).
			WithExec([]string{"-verbose", "-port", "9000", "-hooks", "hooks.json"}, dagger.ContainerWithExecOpts{ExperimentalPrivilegedNesting: true}).
			AsService()

		tunnel, err := client.Host().Tunnel(svc).Start(ctx)
		if err != nil {
			log.Printf("failed to start webhook container: %s", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer func() {
			log.Println("stopping service")
			svc.Stop(ctx)
			log.Println("stopping tunnel")
			tunnel.Stop(ctx)
		}()

		endpoint, err := tunnel.Endpoint(ctx, dagger.ServiceEndpointOpts{Scheme: "http"})
		if err != nil {
			log.Printf("failed to obtain service endpoint: %s", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		u, err := url.Parse(endpoint)
		if err != nil {
			log.Printf("failed to parse endpoint: %s", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		log.Printf("proxying request to %s", endpoint)

		httputil.NewSingleHostReverseProxy(u).ServeHTTP(w, r)
	}
}

func webhookContainer(c *dagger.Client) *dagger.Container {
	// TODO download the right binary for $PLATFORM/$ARCH
	return c.Container().From("ubuntu:lunar").
		WithExec([]string{"sh", "-c", "apt update && apt install -y wget"}).
		WithExec([]string{"wget", "-q", "https://github.com/adnanh/webhook/releases/download/2.8.1/webhook-linux-amd64.tar.gz"}).
		WithExec([]string{"tar", "-C", "/usr/local/bin", "--strip-components", "1", "-xf", "webhook-linux-amd64.tar.gz", "webhook-linux-amd64/webhook"}).
		WithEntrypoint([]string{"/usr/local/bin/webhook"})
}
