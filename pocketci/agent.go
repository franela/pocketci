package pocketci

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"
	"time"

	"dagger.io/dagger"
	"gopkg.in/yaml.v3"

	"github.com/bmatcuk/doublestar"
	"github.com/google/go-github/v61/github"
	"github.com/iancoleman/strcase"
)

var (
	ErrPushEventIgnored = errors.New("push event ignored due to duplication")
)

const DaggerVersion = "0.12.5"

type Secret struct {
	Name    string `yaml:"name"`
	FromEnv string `yaml:"from-env"`
}

type Spec struct {
	ModulePath string   `yaml:"module-path"`
	Events     []string `yaml:"events"`
	Paths      []string `yaml:"paths"`
	Secrets    []Secret `yaml:"secrets"`
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
	)
	switch ghEvent := githubEvent.(type) {
	case *github.PullRequestEvent:
		gitSha = *ghEvent.PullRequest.Head.SHA
		repository = *ghEvent.Repo.FullName
		ref = strings.TrimPrefix(*ghEvent.PullRequest.Head.Ref, "refs/heads/")
		baseRef = strings.TrimPrefix(*ghEvent.PullRequest.Base.Ref, "refs/heads/")
		baseSha = *ghEvent.PullRequest.Base.SHA
	case *github.PushEvent:
		gitSha = *ghEvent.After
		repository = *ghEvent.Repo.FullName
		ref = strings.TrimPrefix(*ghEvent.Ref, "refs/heads/")
	default:
		return nil, fmt.Errorf("received event of type %T that is not yet supported", ghEvent)
	}

	fullRepo := strings.Split(repository, "/")
	repo := fullRepo[len(fullRepo)-1]

	ct := BaseContainer(agent.dag).
		WithEnvVariable("CACHE_BUST", time.Now().String()).
		WithMountedSecret("/root/.netrc", netrc)

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

	return &Event{
		EventType:      event.EventType,
		Changes:        strings.Split(strings.TrimSuffix(filesChanged, "\n"), "\n"),
		RepositoryName: repo,
		RepoContents:   dir,
		Payload:        event.Payload,
	}, nil
}

func (agent *Agent) HandleGithub(ctx context.Context, netrc *dagger.Secret, ghEvent *GithubEvent) error {
	event, err := agent.GithubClone(ctx, netrc, ghEvent)
	if err != nil {
		return err
	}

	cfg, err := parsePocketciConfig(ctx, event.RepoContents.File("pocketci.yaml"))
	if err != nil {
		return fmt.Errorf("failed to parse `pocketci.yaml`: %w", err)
	}

	// we only continue if both the event type and path pattern match
	if !slices.Contains(cfg.Events, event.EventType) {
		slog.Info(fmt.Sprintf("event %s does not match any of %+v", event.EventType, cfg.Events))
		return nil
	}
	if len(cfg.Paths) != 0 && !Match(event.Changes, cfg.Paths...) {
		slog.Info(fmt.Sprintf("changes do not match any of the specified paths: %+v", cfg.Paths))
		return nil
	}

	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}

	stdout, err := AgentContainer(agent.dag).
		WithEnvVariable("CACHE_BUST", time.Now().String()).
		WithEnvVariable("DAGGER_CLOUD_TOKEN", os.Getenv("DAGGER_CLOUD_TOKEN")).
		WithDirectory("/"+event.RepositoryName, event.RepoContents).
		WithWorkdir("/"+event.RepositoryName).
		WithNewFile("/payload.json", string(payload)).
		With(func(c *dagger.Container) *dagger.Container {
			args := []string{"call", "-m", cfg.ModulePath, "--progress", "plain", "dispatch", "--src", ".", "--event-trigger", "/payload.json"}
			for _, secret := range cfg.Secrets {
				c = c.WithSecretVariable(secret.FromEnv, agent.dag.SetSecret(secret.Name, os.Getenv(secret.FromEnv)))
				args = append(args, fmt.Sprintf("--%s", strcase.ToKebab(secret.Name)))
				args = append(args, fmt.Sprintf("env:%s", secret.FromEnv))
			}
			return c.WithExec(args, dagger.ContainerWithExecOpts{
				ExperimentalPrivilegedNesting: true,
				UseEntrypoint:                 true,
			})
		}).
		Stdout(ctx)

	fmt.Println(stdout)
	return err
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
		WithExec([]string{"sh", "-c", fmt.Sprintf(`cd / && DAGGER_VERSION="%s" curl -L https://dl.dagger.io/dagger/install.sh | DAGGER_VERSION="%s" sh`, DaggerVersion, DaggerVersion)}).
		WithEntrypoint([]string{"/bin/dagger"})
}
