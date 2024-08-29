package pocketci

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"dagger.io/dagger"
	"golang.org/x/sync/errgroup"
	"gopkg.in/yaml.v3"

	"github.com/bmatcuk/doublestar"
	"github.com/google/go-github/v61/github"
	"github.com/iancoleman/strcase"
)

var (
	ErrPushEventIgnored = errors.New("push event ignored due to duplication")
)

const DaggerVersion = "0.12.5"

type Event struct {
	EventType      string   `json:"event_type"`
	Filter         string   `json:"filter"`
	Changes        []string `json:"changes"`
	RepositoryName string   `json:"repo_name"`

	ContextVariables map[string]string `json:"-"`
	RepoContents     *dagger.Directory `json:"-"`
	Ref              string            `json:"-"`
	BaseRef          string            `json:"-"`

	// Payload is the payload of the webhook in JSON format.
	Payload json.RawMessage `json:"payload"`
}

type GithubEvent struct {
	EventType string
	Payload   json.RawMessage `json:"payload"`
}

type Secret struct {
	Name    string `yaml:"name"`
	FromEnv string `yaml:"from-env"`
}

type Spec struct {
	ModulePath   string       `yaml:"module-path"`
	EventTrigger EventTrigger `yaml:"events"`
	Paths        []string     `yaml:"paths"`
	Secrets      []Secret     `yaml:"secrets"`
}

type EventTrigger struct {
	PullRequest []string `json:"pull_request"`
	Push        []string `json:"push"`
}

type Agent struct {
	dag *dagger.Client
}

func NewAgent(dag *dagger.Client) *Agent {
	return &Agent{
		dag: dag,
	}
}

func (agent *Agent) CreateGithubSecret(username, password string) *dagger.Secret {
	return agent.dag.SetSecret("github_auth", fmt.Sprintf("machine github.com login %s password %s", username, password))
}

func (agent *Agent) GithubClone(ctx context.Context, netrc *dagger.Secret, event *GithubEvent) (*Event, error) {
	githubEvent, err := github.ParseWebHook(event.EventType, event.Payload)
	if err != nil {
		return nil, err
	}

	var (
		gitSha     string
		repository string
		ref        string
		baseRef    string
		baseSha    string
		prNumber   int
		filter     string
	)
	switch ghEvent := githubEvent.(type) {
	case *github.PullRequestEvent:
		gitSha = *ghEvent.PullRequest.Head.SHA
		repository = *ghEvent.Repo.FullName
		ref = strings.TrimPrefix(*ghEvent.PullRequest.Head.Ref, "refs/heads/")
		baseRef = strings.TrimPrefix(*ghEvent.PullRequest.Base.Ref, "refs/heads/")
		baseSha = *ghEvent.PullRequest.Base.SHA
		prNumber = *ghEvent.Number
		filter = *ghEvent.Action
	case *github.PushEvent:
		gitSha = *ghEvent.After
		repository = *ghEvent.Repo.FullName
		ref = strings.TrimPrefix(*ghEvent.Ref, "refs/heads/")
		filter = ref
	default:
		return nil, fmt.Errorf("received event of type %T that is not yet supported", ghEvent)
	}

	fullRepo := strings.Split(repository, "/")
	repo := fullRepo[len(fullRepo)-1]

	ct := BaseContainer(agent.dag).
		WithEnvVariable("CACHE_BUST", time.Now().String()).
		WithMountedSecret("/root/.netrc", netrc)

	slog.Info("cloning repository", slog.String("repository", repository), slog.String("ref", ref), slog.String("commit_sha", gitSha))
	// NOTE: it is important that we check out the repository with at least some
	// history. We need at least two commits (or just one if its the initial commit)
	// in order to compute the list of changes of the latest commit. We use
	// a manual git clone instead of dagger's builtin dag.Git function because
	// of this requirement.
	dir, err := ct.
		WithExec([]string{"git", "clone", "--single-branch", "--branch", ref, "--depth", "10", "https://github.com/" + repository}).
		WithWorkdir("/" + repo).
		WithExec([]string{"git", "checkout", gitSha}).
		Directory("/" + repo).
		Sync(ctx)
	if err != nil {
		return nil, err
	}

	var filesChanged string
	// if there is a baseRef then we need to make a comparisson of all the files
	// being changed
	if baseRef != "" {
		filesChanged, err = ct.
			WithDirectory("/repo", dir).
			WithWorkdir("/repo").
			WithExec([]string{"git", "fetch", "origin", baseRef}).
			WithExec([]string{"git", "diff-tree", "--no-commit-id", "--name-only", "-r", "HEAD", baseSha}).
			Stdout(ctx)
		if err != nil {
			return nil, err
		}
	} else {
		filesChanged, err = ct.
			WithDirectory("/repo", dir).
			WithWorkdir("/repo").
			WithExec([]string{"git", "diff-tree", "--no-commit-id", "--name-only", "-r", "HEAD"}).
			Stdout(ctx)
	}
	if err != nil {
		return nil, err
	}

	slog.Info("computed files changed for repository", slog.String("repository", repository), slog.Int("files_changed", len(filesChanged)))

	variables := map[string]string{
		"GITHUB_SHA":        gitSha,
		"GITHUB_ACTIONS":    "true",
		"GITHUB_EVENT_NAME": event.EventType,
		"GITHUB_EVENT_PATH": "/raw-payload.json",
	}
	if baseRef != "" {
		variables["GITHUB_REF"] = "refs/pull/" + strconv.Itoa(prNumber) + "/merge"
	}

	return &Event{
		EventType:        event.EventType,
		Filter:           filter,
		Changes:          strings.Split(strings.TrimSuffix(filesChanged, "\n"), "\n"),
		RepositoryName:   repo,
		RepoContents:     dir,
		ContextVariables: variables,
		Payload:          event.Payload,
		BaseRef:          baseRef,
		Ref:              ref,
	}, nil
}

func shouldHandle(cfg *Spec, event *Event) bool {
	// we only continue if both the event type and path pattern match
	if event.EventType != "pull_request" && event.EventType != "push" {
		slog.Info(fmt.Sprintf("event %s does not match any of pull_request,push", event.EventType), slog.String("event_type", event.EventType))
		return false
	}

	if len(cfg.Paths) != 0 && !Match(event.Changes, cfg.Paths...) {
		slog.Info(fmt.Sprintf("changes do not match any of the specified paths: %+v", cfg.Paths), slog.String("event_type", event.EventType))
		return false
	}

	switch event.EventType {
	case "pull_request":
		if len(cfg.EventTrigger.PullRequest) > 0 && !slices.Contains(cfg.EventTrigger.PullRequest, event.BaseRef) {
			slog.Info("base ref is not in the allow list", slog.String("event_type", event.EventType), slog.String("base_ref", event.BaseRef))
			return false
		}
	case "push":
		if len(cfg.EventTrigger.Push) > 0 && !slices.Contains(cfg.EventTrigger.Push, event.Ref) {
			slog.Info("ref is not in the allow list", slog.String("event_type", event.EventType), slog.String("ref", event.Ref))
			return false
		}
	}

	return true
}

func (agent *Agent) HandleGithub(ctx context.Context, netrc *dagger.Secret, ghEvent *GithubEvent) error {
	slog.Info("received event from GitHub", slog.String("event_type", ghEvent.EventType))

	event, err := agent.GithubClone(ctx, netrc, ghEvent)
	if err != nil {
		return err
	}

	cfg, err := parsePocketciConfig(ctx, event.RepoContents.File("pocketci.yaml"))
	if err != nil {
		return fmt.Errorf("failed to parse `pocketci.yaml`: %w", err)
	}

	if !shouldHandle(cfg, event) {
		return nil
	}

	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal internal event: %s", err)
	}

	functions, err := dispatcherFunction(ctx, "github", event.EventType, event.Filter, event.RepoContents.Directory(cfg.ModulePath).AsModule().Initialize())
	if err != nil {
		return fmt.Errorf("failed to get dispatcher function: %s", err)
	}

	var g errgroup.Group
	for _, function := range functions {
		g.Go(func() error {
			fn := function
			slog.Info("launching pocketci agent container dispatch call", slog.String("repository", event.RepositoryName), slog.String("function", fn.name), slog.String("event_type", ghEvent.EventType), slog.String("filter", event.Filter))

			call := fmt.Sprintf("dagger call -m %s --progress plain %s %s --src . --event-trigger /payload.json", cfg.ModulePath, fn.name, fn.args)
			stdout, err := AgentContainer(agent.dag).
				WithEnvVariable("CACHE_BUST", time.Now().String()).
				WithEnvVariable("DAGGER_CLOUD_TOKEN", os.Getenv("DAGGER_CLOUD_TOKEN")).
				WithDirectory("/"+event.RepositoryName, event.RepoContents).
				WithWorkdir("/"+event.RepositoryName).
				WithNewFile("/raw-payload.json", string(ghEvent.Payload)).
				WithNewFile("/payload.json", string(payload)).
				WithEnvVariable("CI", "pocketci").
				With(func(c *dagger.Container) *dagger.Container {
					// set contextual variables used by the dagger CLI to report labels
					// to dagger cloud
					for key, val := range event.ContextVariables {
						c = c.WithEnvVariable(key, val)
					}
					script := fmt.Sprintf("unset TRACEPARENT;unset OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf;unset OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:38015;unset OTEL_EXPORTER_OTLP_TRACES_PROTOCOL=http/protobuf;unset OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=http://127.0.0.1:38015/v1/traces;unset OTEL_EXPORTER_OTLP_TRACES_LIVE=1;unset OTEL_EXPORTER_OTLP_LOGS_PROTOCOL=http/protobuf;unset OTEL_EXPORTER_OTLP_LOGS_ENDPOINT=http://127.0.0.1:38015/v1/logs;unset OTEL_EXPORTER_OTLP_METRICS_PROTOCOL=http/protobuf;unset OTEL_EXPORTER_OTLP_METRICS_ENDPOINT=http://127.0.0.1:38015/v1/metrics; %s", call)
					for _, secret := range cfg.Secrets {
						c = c.WithSecretVariable(secret.FromEnv, agent.dag.SetSecret(secret.Name, os.Getenv(secret.FromEnv)))
						script = fmt.Sprintf("%s --%s env:%s", script, strcase.ToKebab(secret.Name), secret.FromEnv)
					}
					return c.WithExec([]string{"sh", "-c", script}, dagger.ContainerWithExecOpts{
						ExperimentalPrivilegedNesting: true,
					})
				}).
				Stdout(ctx)
			if err != nil {
				return err
			}

			fmt.Printf("$ %s\n%s", call, stdout)
			return nil
		})
	}
	return g.Wait()
}

func parsePocketciConfig(ctx context.Context, config *dagger.File) (*Spec, error) {
	contents, err := config.Contents(ctx)
	if err != nil {
		return nil, err
	}

	cfg := &Spec{}
	if err := yaml.Unmarshal([]byte(contents), &cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

func Match(files []string, patterns ...string) bool {
	for _, file := range files {
		for _, pattern := range patterns {
			match, err := doublestar.PathMatch(pattern, file)
			if err != nil {
				continue
			}
			if match {
				return true
			}
		}
	}
	return false
}

func BaseContainer(c *dagger.Client) *dagger.Container {
	return c.Container().From("ubuntu:lunar").
		WithExec([]string{"sh", "-c", "apt update && apt install -y curl wget git"})
}

func AgentContainer(c *dagger.Client) *dagger.Container {
	return BaseContainer(c).
		WithExec([]string{"sh", "-c", fmt.Sprintf(`cd / && DAGGER_VERSION="%s" curl -L https://dl.dagger.io/dagger/install.sh | DAGGER_VERSION="%s" sh`, DaggerVersion, DaggerVersion)})
}
