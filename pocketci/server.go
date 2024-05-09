package pocketci

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"

	"dagger.io/dagger"
)

const GithubEventTypeHeader = "X-Github-Event"

type GithubEvent struct {
	EventType string          `json:"event_type"`
	Payload   json.RawMessage `json:"payload"`
	Changes   []string        `json:"changes"`
}

type Server struct {
	agent *Agent
	opts  ServerOptions
}

type ServerOptions struct {
	GithubUsername string
	GithubPassword string
}

func NewServer(dag *dagger.Client, opts ServerOptions) (*Server, error) {
	// warmup the container that will be used for each request
	if _, err := WebhookContainer(dag).Sync(context.Background()); err != nil {
		return nil, fmt.Errorf("warmup failed: %w", err)
	}

	return &Server{
		agent: NewAgent(dag),
		opts:  opts,
	}, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("failed to get request body: %s", err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	r.Body = io.NopCloser(bytes.NewBuffer(b))

	go func() {
		githubEvent := &GithubEvent{
			EventType: r.Header.Get(GithubEventTypeHeader),
			Payload:   json.RawMessage(b),
		}
		ctx := context.Background()
		s.HandleRequest(ctx, githubEvent, r.WithContext(ctx))
	}()

	w.WriteHeader(http.StatusAccepted)
}

// TODO: Support other VCS
func (s *Server) HandleRequest(ctx context.Context, event *GithubEvent, r *http.Request) error {
	svc, err := s.agent.HandleGithub(ctx, s.agent.CreateGithubSecret(s.opts.GithubUsername, s.opts.GithubPassword), event)
	if err != nil {
		log.Printf("failed to handle github request: %s\n", err)
		return err
	}

	tunnel, err := s.agent.dag.Host().Tunnel(svc).Start(ctx)
	if err != nil {
		log.Printf("failed to start webhook container: %s", err)
		return err
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
		return err
	}

	u, err := url.Parse(endpoint)
	if err != nil {
		log.Printf("failed to parse endpoint: %s", err)
		return err
	}

	log.Printf("proxying request to %s", endpoint)
	res := httptest.NewRecorder()
	httputil.NewSingleHostReverseProxy(u).ServeHTTP(res, r)
	return nil
}
