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
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"

	"dagger.io/dagger"
	"gopkg.in/yaml.v3"
)

var (
	hooksPath = flag.String("hooks", "", "path to an optional hooks.yaml file. If not provided it will start in gitCloneProxy mode")
	repos     = flag.String("repos", "", "allow-list of owner/repo. If not specified all repos are valid. If `hooks` is specified, this will be ignored")
	async     = flag.Bool("async", false, "if true, the webhook will be executed asynchronously")
)

func main() {
	flag.Parse()

	ctx := context.Background()
	client, err := dagger.Connect(ctx, dagger.WithLogOutput(os.Stderr))
	if err != nil {
		log.Fatalf("failed to connect to dagger: %v", err)
	}
	defer client.Close()

	// we're warming up the webhook container here so we don't make the
	// user wait for the first request to be served
	if _, err = webhookContainer(client).Sync(ctx); err != nil {
		log.Fatalf("failed to build webhook: %v", err)
	}

	mux := http.NewServeMux()

	if *hooksPath != "" {
		log.Println("starting reverse proxy mode")

		hooksFile := client.Host().File(*hooksPath)
		hooks, err := hooksFile.Contents(ctx)
		if err != nil {
			log.Fatalf("failed to read hooks file: %v", err)
		}
		if hooks == "" {
			log.Fatalf("hooks file is empty")
		}

		mux.HandleFunc("/", reverseProxy(ctx, client, *async, hooksFile))
	} else {
		log.Println("starting git proxy mode")

		mux.HandleFunc("/", gitCloneProxy(*async))
	}

	srv := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}
	log.Println("starting proxy server in port 8080")
	if err = srv.ListenAndServe(); err != nil {
		log.Printf("serve exited with: %v", err)
	}
}

func reverseProxy(ctx context.Context, client *dagger.Client, async bool, hooks *dagger.File) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		svc := webhookContainer(client).
			WithFile("/hooks/hooks.yaml", hooks).
			WithWorkdir("/hooks").
			WithExposedPort(9000).
			WithExec([]string{"-verbose", "-port", "9000", "-hooks", "hooks.yaml"}, dagger.ContainerWithExecOpts{ExperimentalPrivilegedNesting: true}).
			AsService()

		handleRequest(ctx, async, client, svc, w, r)
	}
}

type GithubWebhook struct {
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	After string `json:"after"`
}

// gitCloneProxy returns a handler that will first clone a git repository into
// the specified directory and then proxy the request to the reverse proxy.
func gitCloneProxy(async bool) http.HandlerFunc {
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

		// we always want to execute webhooks in a synchronous way from our point
		// of view, so we always force `include-command-output-in-response` to true
		ctx = context.Background()
		repo := client.Git("https://github.com/" + gh.Repository.FullName).Commit(gh.After).Tree()
		repo, err = forceSyncOutput(ctx, repo)
		if err != nil {
			log.Printf("failed to force include-command-output-in-response: %s", err.Error())
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		fullRepo := strings.Split(gh.Repository.FullName, "/")
		repoName := fullRepo[len(fullRepo)-1]
		svc := webhookContainer(client).
			WithDirectory("/"+repoName, repo).
			WithWorkdir("/"+repoName).
			WithExposedPort(9000).
			WithExec([]string{"-verbose", "-port", "9000", "-hooks", "hooks.yaml"}, dagger.ContainerWithExecOpts{ExperimentalPrivilegedNesting: true}).
			AsService()
		handleRequest(ctx, async, client, svc, w, r)
	}
}

func handleRequest(ctx context.Context, async bool, client *dagger.Client, svc *dagger.Service, w http.ResponseWriter, r *http.Request) {
	// if `async` is set to true, what we want is to do a fire and forget
	// but if it's set to false we want to send the request sync and wait
	if !async {
		proxyRequest(ctx, client, svc, w, r)
		return
	}

	log.Printf("proxying request async")
	go func() {
		ctx := context.Background()
		res := httptest.NewRecorder()
		r = r.WithContext(ctx)
		proxyRequest(ctx, client, svc, res, r)
		log.Printf("async request finished with status %d and body: %s", res.Code, res.Body.String())
	}()
	log.Printf("responding with a default 202")
	w.WriteHeader(http.StatusAccepted)
}

func proxyRequest(ctx context.Context, client *dagger.Client, svc *dagger.Service, w http.ResponseWriter, r *http.Request) {
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

func forceSyncOutput(ctx context.Context, repo *dagger.Directory) (*dagger.Directory, error) {
	repoHooks, err := repo.File("hooks.yaml").Contents(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to read hooks file: %w", err)
	}

	hooksDef := []map[string]any{}
	if err = yaml.Unmarshal([]byte(repoHooks), &hooksDef); err != nil {
		return nil, fmt.Errorf("failed to decode hooks file: %w", err)
	}

	for _, hook := range hooksDef {
		hook["include-command-output-in-response"] = true
	}

	syncHooks, err := yaml.Marshal(hooksDef)
	if err != nil {
		return nil, fmt.Errorf("failed to encode hooks file: %w", err)
	}
	return repo.WithNewFile("hooks.yaml", string(syncHooks)), nil
}

func webhookContainer(c *dagger.Client) *dagger.Container {
	// TODO download the right binary for $PLATFORM/$ARCH
	return c.Container().From("ubuntu:lunar").
		WithExec([]string{"sh", "-c", "apt update && apt install -y wget"}).
		WithExec([]string{"wget", "-q", "https://github.com/adnanh/webhook/releases/download/2.8.1/webhook-linux-amd64.tar.gz"}).
		WithExec([]string{"tar", "-C", "/usr/local/bin", "--strip-components", "1", "-xf", "webhook-linux-amd64.tar.gz", "webhook-linux-amd64/webhook"}).
		WithEntrypoint([]string{"/usr/local/bin/webhook"})
}
