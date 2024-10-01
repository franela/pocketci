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
	"os"
	"strconv"
	"strings"
	"time"

	"dagger.io/dagger"
)

const GithubEventTypeHeader = "X-Github-Event"

type Server struct {
	orchestrator    *Orchestrator
	githubSignature string
}

// TODO: move away into a proper `Config` structure for the server
type ServerOptions struct {
	GithubUsername  string
	GithubPassword  string
	GithubSignature string
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
	}

	return s, nil
}

type PipelineRequest struct {
	RunsOn         string `json:"runs_on"`
	Name           string `json:"name"`
	Exec           string `json:"exec"`
	RepositoryName string `json:"repository_name"`
	Ref            string `json:"ref"`
	SHA            string `json:"sha"`
	BaseRef        string `json:"base_ref"`
	BaseSHA        string `json:"base_sha"`
	PrNumber       int    `json:"pr_number"`
	Module         string `json:"module"`
	Workdir        string `json:"workdir"`
	EventType      string `json:"event_type"`
}

func (s *Server) PipelineHandler(w http.ResponseWriter, r *http.Request) {
	req := &PipelineRequest{}
	if err := json.NewDecoder(r.Body).Decode(req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusAccepted)

	go func() {

		ct := BaseContainer(s.orchestrator.dag).
			WithEnvVariable("CACHE_BUST", time.Now().String()).
			WithMountedSecret("/root/.netrc", s.orchestrator.GithubNetrc)
		repo, _, err := cloneAndDiff(r.Context(), ct, "https://github.com/"+req.RepositoryName, req.Ref, req.SHA, req.BaseRef, req.BaseSHA)
		if err != nil {
			http.Error(w, fmt.Errorf("could not clond and diff repository: %s", err).Error(), http.StatusInternalServerError)
			return
		}

		vars := map[string]string{
			"GITHUB_SHA":        req.SHA,
			"GITHUB_ACTIONS":    "true",
			"GITHUB_EVENT_NAME": req.EventType,
		}
		if req.BaseRef != "" {
			vars["GITHUB_REF"] = "refs/pull/" + strconv.Itoa(req.PrNumber) + "/merge"
		}

		slog.Info("launching pocketci agent container",
			slog.String("repository_name", req.RepositoryName), slog.String("pipeline", req.Name),
			slog.String("ref", req.Ref), slog.String("base_ref", req.BaseRef),
			slog.String("sha", req.SHA), slog.String("base_sha", req.BaseSHA),
			slog.String("module", req.Module), slog.String("workdir", req.Workdir),
			slog.String("exec", req.Exec), slog.String("runs_on", req.RunsOn))

		call := fmt.Sprintf("dagger call -m %s --progress plain %s", req.Module, req.Exec)
		stdout, err := AgentContainer(s.orchestrator.dag).
			WithEnvVariable("CACHE_BUST", time.Now().String()).
			WithEnvVariable("DAGGER_CLOUD_TOKEN", os.Getenv("DAGGER_CLOUD_TOKEN")).
			WithDirectory("/"+req.RepositoryName, repo).
			WithWorkdir("/"+req.RepositoryName).
			WithEnvVariable("CI", "pocketci").
			With(func(c *dagger.Container) *dagger.Container {
				for key, val := range vars {
					c = c.WithEnvVariable(key, val)
				}
				script := fmt.Sprintf("unset TRACEPARENT;unset OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf;unset OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:38015;unset OTEL_EXPORTER_OTLP_TRACES_PROTOCOL=http/protobuf;unset OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=http://127.0.0.1:38015/v1/traces;unset OTEL_EXPORTER_OTLP_TRACES_LIVE=1;unset OTEL_EXPORTER_OTLP_LOGS_PROTOCOL=http/protobuf;unset OTEL_EXPORTER_OTLP_LOGS_ENDPOINT=http://127.0.0.1:38015/v1/logs;unset OTEL_EXPORTER_OTLP_METRICS_PROTOCOL=http/protobuf;unset OTEL_EXPORTER_OTLP_METRICS_ENDPOINT=http://127.0.0.1:38015/v1/metrics; %s", call)
				return c.WithExec([]string{"sh", "-c", script}, dagger.ContainerWithExecOpts{
					ExperimentalPrivilegedNesting: true,
				})
			}).
			Stdout(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		fmt.Println(stdout)
	}()

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
