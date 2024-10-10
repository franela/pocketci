package pocketci

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"dagger.io/dagger"
)

const GithubEventTypeHeader = "X-Github-Event"

type Server struct {
	orchestrator    *Orchestrator
	githubSignature string

	mu        sync.Mutex
	pipelines []*CreatePipelineRequest
}

// TODO: move away into a proper `Config` structure for the server
type ServerOptions struct {
	GithubUsername  string
	GithubPassword  string
	GithubSignature string
	WatchRepository string
	WatchInterval   time.Duration
}

func NewServer(dag *dagger.Client, opts ServerOptions) (*Server, error) {
	// warmup the container that will be used for each request
	// TODO: Should git operations be handled outside of Dagger? Could that have
	// a positive perf impact that is worth it?
	if _, err := AgentContainer(dag).Sync(context.Background()); err != nil {
		return nil, fmt.Errorf("warmup failed: %w", err)
	}

	s := &Server{
		orchestrator: &Orchestrator{
			Dispatcher: &LocalDispatcher{
				dag:         dag,
				parallelism: 2,
			},
			dag:         dag,
			GithubNetrc: dag.SetSecret("github_auth", fmt.Sprintf("machine github.com login %s password %s", opts.GithubUsername, opts.GithubPassword)),
		},
		githubSignature: opts.GithubSignature,
		pipelines:       []*CreatePipelineRequest{},
	}

	if opts.WatchRepository != "" {
		go s.orchestrator.watchRepository(context.Background(), opts.WatchRepository, opts.WatchInterval)
	}

	return s, nil
}

// TODO: generalize this code to support other VCS and event matchers in general
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	// Github webhook
	case r.Header.Get("X-Hub-Signature") != "":
		sig := r.Header.Get("X-Hub-Signature")
		if sig == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		if err := validateGithubSignature(sig, s.githubSignature); err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}

		b, err := io.ReadAll(r.Body)
		if err != nil {
			slog.Debug("failed to get request body", slog.String("error", err.Error()))
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		r.Body = io.NopCloser(bytes.NewBuffer(b))

		go func() {
			wh := &Webhook{
				Vendor:    GithubVendor,
				EventType: r.Header.Get(GithubEventTypeHeader),
				Payload:   json.RawMessage(b),
			}

			ctx := context.Background()
			if err := s.orchestrator.Handle(ctx, wh); err != nil {
				slog.Error("failed to handle github request", slog.String("error", err.Error()))
			}
		}()

		w.WriteHeader(http.StatusAccepted)
	}
}

func validateGithubSignature(signature string, secret string) error {
	signature = strings.TrimPrefix(signature, "sha1=")

	mac := hmac.New(sha1.New, []byte(secret))

	_, err := mac.Write([]byte(signature))
	if err != nil {
		return err
	}

	actualMAC := hex.EncodeToString(mac.Sum(nil))

	if hmac.Equal([]byte(signature), []byte(actualMAC)) {
		return err
	}

	return nil
}
