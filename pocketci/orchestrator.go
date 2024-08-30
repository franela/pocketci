package pocketci

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"

	"dagger.io/dagger"
	"github.com/bmatcuk/doublestar"
	"github.com/google/go-github/v61/github"
	"gopkg.in/yaml.v3"
)

const (
	GithubVendor = "github"

	DaggerVersion = "0.12.5"
)

type Orchestrator struct {
	Dispatcher Dispatcher
	dag        *dagger.Client

	GithubNetrc *dagger.Secret
}

type Webhook struct {
	Vendor    string
	EventType string
	Payload   json.RawMessage
}

func (o *Orchestrator) Handle(ctx context.Context, wh *Webhook) error {
	// The orchestrator receives a webhook and it first checks to see if its from
	// a supported vendor.
	switch wh.Vendor {
	case GithubVendor:
		if !slices.Contains([]string{GithubPullRequest, GithubPush, GithubRelease}, wh.EventType) {
			return fmt.Errorf("event %s is not supported", wh.EventType)
		}
		return o.HandleGithub(ctx, wh)
	default:
		return fmt.Errorf("vendor %s is not supported", wh.Vendor)
	}
}

func (o *Orchestrator) HandleGithub(ctx context.Context, wh *Webhook) error {
	event, err := o.handleGithubEvent(ctx, wh.EventType, wh.Payload)
	if err != nil {
		return err
	}

	spec, err := parseRepositorySpec(ctx, event.Repository.File("pocketci.yaml"))
	if err != nil {
		slog.Info("parsing repository pocketci.yaml failed, using default values", slog.String("repository", event.RepositoryName), slog.String("error", err.Error()))
		spec = &Spec{ModulePath: "./ci"}
	}

	functions, err := matchFunctions(ctx, GithubVendor, event.EventType, event.Filter, event.Changes, event.Repository.Directory(spec.ModulePath).AsModule().Initialize())
	if err != nil {
		return fmt.Errorf("failed to get dispatcher function: %s", err)
	}

	if len(functions) == 0 {
		slog.Info("event did not match any functions", slog.String("repository", event.RepositoryName), slog.String("event", event.EventType), slog.String("vendor", GithubVendor), slog.String("filter", event.Filter))
		return nil
	}

	return o.Dispatcher.Dispatch(ctx, spec, functions, &Event{
		RepositoryName: event.EventType,
		Repository:     event.Repository,
		Changes:        event.Changes,
		Payload:        wh.Payload,
		Vendor:         GithubVendor,
		EventType:      wh.EventType,
		Filter:         event.Filter,
		EnvVariables:   event.Variables,
	})
}

func (o *Orchestrator) handleGithubEvent(ctx context.Context, eventType string, payload json.RawMessage) (*GithubEvent, error) {
	githubEvent, err := github.ParseWebHook(eventType, payload)
	if err != nil {
		return nil, err
	}

	gh := &GithubEvent{
		Payload:   payload,
		EventType: eventType,
	}
	switch ghEvent := githubEvent.(type) {
	case *github.PullRequestEvent:
		gh.SHA = *ghEvent.PullRequest.Head.SHA
		gh.RepositoryName = *ghEvent.Repo.FullName
		gh.Ref = strings.TrimPrefix(*ghEvent.PullRequest.Head.Ref, "refs/heads/")
		gh.BaseRef = strings.TrimPrefix(*ghEvent.PullRequest.Base.Ref, "refs/heads/")
		gh.BaseSHA = *ghEvent.PullRequest.Base.SHA
		gh.PrNumber = *ghEvent.Number

		// For pull requests the filter is the differnet actions that happen
		// on a pull request.
		gh.Filter = *ghEvent.Action
	case *github.PushEvent:
		gh.SHA = *ghEvent.After
		gh.RepositoryName = *ghEvent.Repo.FullName
		gh.Ref = strings.TrimPrefix(*ghEvent.Ref, "refs/heads/")

		// In the case of push events the Filter of the event is the actual ref.
		// This can be a branch or tag.
		gh.Filter = gh.Ref
	default:
		return nil, fmt.Errorf("received event of type %T that is not yet supported", ghEvent)
	}

	ct := BaseContainer(o.dag).
		WithEnvVariable("CACHE_BUST", time.Now().String()).
		WithMountedSecret("/root/.netrc", o.GithubNetrc)
	gh.Repository, gh.Changes, err = cloneAndDiff(ctx, ct, "https://github.com/"+gh.RepositoryName, gh.Ref, gh.SHA, gh.BaseRef, gh.BaseSHA)
	if err != nil {
		return nil, fmt.Errorf("could not clond and diff repository: %s", err)
	}

	gh.Variables = map[string]string{
		"GITHUB_SHA":        gh.SHA,
		"GITHUB_ACTIONS":    "true",
		"GITHUB_EVENT_NAME": gh.EventType,
		"GITHUB_EVENT_PATH": "/raw-payload.json",
	}
	if gh.BaseRef != "" {
		gh.Variables["GITHUB_REF"] = "refs/pull/" + strconv.Itoa(gh.PrNumber) + "/merge"
	}

	return gh, nil
}

// cloneAndDiff clones the repository at `ref` and checks out `sha`. It returns
// its contents plus the list of files that changed. If `baseRef` is specified
// we compare the ref:sha against it. If not we compare HEAD against the previous
// commit.
// `ct` is a container with git and relevant credentials already configured.
func cloneAndDiff(ctx context.Context, ct *dagger.Container, url, ref, sha, baseRef, baseSha string) (*dagger.Directory, []string, error) {
	slog.Info("cloning repository", slog.String("repository", url), slog.String("ref", ref), slog.String("sha", sha), slog.String("base_ref", baseRef), slog.String("base_sha", baseSha))

	// NOTE: it is important that we check out the repository with at least some
	// history. We need at least two commits (or just one if its the initial commit)
	// in order to compute the list of changes of the latest commit. We use
	// a manual git clone instead of dagger's builtin dag.Git function because
	// of this requirement.
	dir, err := ct.
		WithExec([]string{"git", "clone", "--single-branch", "--branch", ref, "--depth", "10", url, "/app"}).
		WithWorkdir("/app").
		WithExec([]string{"git", "checkout", sha}).
		Directory("/app").
		Sync(ctx)
	if err != nil {
		return nil, nil, err
	}

	var filesChanged string
	// if there is a baseRef then we need to make a comparisson of all the files
	// being changed
	if baseRef != "" {
		filesChanged, err = ct.
			WithDirectory("/app", dir).
			WithWorkdir("/app").
			WithExec([]string{"git", "fetch", "origin", baseRef}).
			WithExec([]string{"git", "diff-tree", "--no-commit-id", "--name-only", "-r", "HEAD", baseSha}).
			Stdout(ctx)
		if err != nil {
			return nil, nil, err
		}
	} else {
		filesChanged, err = ct.
			WithDirectory("/app", dir).
			WithWorkdir("/app").
			WithExec([]string{"git", "diff-tree", "--no-commit-id", "--name-only", "-r", "HEAD"}).
			Stdout(ctx)
	}
	if err != nil {
		return nil, nil, err
	}

	return dir, strings.Split(strings.TrimSuffix(filesChanged, "\n"), "\n"), nil
}

type Secret struct {
	Name    string `yaml:"name"`
	FromEnv string `yaml:"from-env"`
}

type Spec struct {
	ModulePath string   `yaml:"module-path"`
	Secrets    []Secret `yaml:"secrets"`
}

type EventTrigger struct {
	PullRequest []string `json:"pull_request"`
	Push        []string `json:"push"`
}

func parseRepositorySpec(ctx context.Context, config *dagger.File) (*Spec, error) {
	contents, err := config.Contents(ctx)
	if err != nil {
		return nil, err
	}

	spec := &Spec{}
	if err := yaml.Unmarshal([]byte(contents), &spec); err != nil {
		return nil, err
	}

	return spec, nil
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
