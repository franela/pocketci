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
	"github.com/bmatcuk/doublestar"
	"github.com/google/go-github/v61/github"
	"gopkg.in/yaml.v3"
)

const (
	GithubVendor = "github"

	DaggerVersion = "0.13.5"
)

type Orchestrator struct {
	Dispatcher Dispatcher
	dag        *dagger.Client

	GithubNetrc *dagger.Secret
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

	module, err := getDispatchModule(ctx, event.Repository.File("pocketci.yaml"))
	if err != nil {
		return err
	}

	fn, err := hasFunction(ctx, event.Repository.Directory(module).AsModule(), "pocketciPipelines", "pipelines", "dispatch")
	if err != nil {
		return err
	}

	// with the function we now need to get the dagger file that it returns
	// containing all the workflows the user has configured
	pipelines, err := o.getPipelines(ctx, event, fn)
	if err != nil {
		return err
	}

	slog.Info("dispatching pipelines", slog.Int("pipelines", len(pipelines)))
	return o.Dispatcher.Dispatch(ctx, GitInfo{Branch: event.Branch, SHA: event.SHA}, pipelines)
}

func (o *Orchestrator) getPipelines(ctx context.Context, event *GithubEvent, fn string) ([]*Pipeline, error) {
	stdout, err := AgentContainer(o.dag).
		WithEnvVariable("CACHE_BUST", time.Now().String()).
		WithEnvVariable("DAGGER_CLOUD_TOKEN", os.Getenv("DAGGER_CLOUD_TOKEN")).
		WithDirectory("/"+event.RepositoryName, event.Repository).
		WithWorkdir("/" + event.RepositoryName).
		With(func(c *dagger.Container) *dagger.Container {
			call := fmt.Sprintf("dagger call -vvv --progress plain %s contents", fn)
			script := fmt.Sprintf("unset TRACEPARENT;unset OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf;unset OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:38015;unset OTEL_EXPORTER_OTLP_TRACES_PROTOCOL=http/protobuf;unset OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=http://127.0.0.1:38015/v1/traces;unset OTEL_EXPORTER_OTLP_TRACES_LIVE=1;unset OTEL_EXPORTER_OTLP_LOGS_PROTOCOL=http/protobuf;unset OTEL_EXPORTER_OTLP_LOGS_ENDPOINT=http://127.0.0.1:38015/v1/logs;unset OTEL_EXPORTER_OTLP_METRICS_PROTOCOL=http/protobuf;unset OTEL_EXPORTER_OTLP_METRICS_ENDPOINT=http://127.0.0.1:38015/v1/metrics; %s", call)
			return c.WithExec([]string{"sh", "-c", script}, dagger.ContainerWithExecOpts{
				ExperimentalPrivilegedNesting: true,
			})
		}).
		Stdout(ctx)
	if err != nil {
		return nil, err
	}

	pipelines := []*Pipeline{}
	if err := json.Unmarshal([]byte(stdout), &pipelines); err != nil {
		return nil, err
	}

	run := []*Pipeline{}
	for _, p := range pipelines {
		// only match pipelines when list of changes is empty or matches the
		// files that changed
		if len(p.Changes) != 0 && !Match(event.Changes, p.Changes...) {
			continue
		}

		p.Repository = event.RepositoryName

		switch {
		case event.PullRequestEvent != nil && p.OnPR && (len(p.Actions) == 0 || slices.Contains(p.Actions, *event.PullRequestEvent.Action)):
			// if the pipeline has also configured a Push trigger that matches
			// the branches then we skip this event to avoid duplicates
			if p.OnPush && (len(p.Branches) == 0 || slices.Contains(p.Branches, *event.PullRequestEvent.PullRequest.Head.Ref)) {
				return nil, errors.New("pull request pipeline is already matched by push event")
			}

			// received a pull request and the pipeline targets the PR
			slog.Debug("pipeline matched on pull request event", slog.String("repository", event.RepositoryName),
				slog.String("action", *event.PullRequestEvent.Action), slog.String("pipeline", p.Name))
			run = append(run, p)
		case event.PushEvent != nil && p.OnPush && (len(p.Branches) == 0 || slices.Contains(p.Branches, event.PushEvent.GetRef()[11:])):
			// received a push event and the pipeline targets push event
			slog.Debug("pipeline matched on push event", slog.String("repository", event.RepositoryName),
				slog.String("ref", *event.PushEvent.Ref), slog.String("pipeline", p.Name))
			run = append(run, p)
		default:
			return nil, errors.New("unhandled event")
		}
	}

	return run, nil
}

func (o *Orchestrator) handleGithubEvent(ctx context.Context, eventType string, payload json.RawMessage) (*GithubEvent, error) {
	githubEvent, err := github.ParseWebHook(eventType, payload)
	if err != nil {
		return nil, err
	}

	var (
		gh = &GithubEvent{
			EventType: eventType,
		}
		baseRef, baseSha string
	)
	switch ghEvent := githubEvent.(type) {
	case *github.PullRequestEvent:
		gh.PullRequestEvent = ghEvent

		gh.SHA = *ghEvent.PullRequest.Head.SHA
		gh.RepositoryName = *ghEvent.Repo.FullName
		gh.Branch = branchName(*ghEvent.PullRequest.Head.Ref)
		baseRef = branchName(*ghEvent.PullRequest.Base.Ref)
		baseSha = *ghEvent.PullRequest.Base.SHA
	case *github.PushEvent:
		gh.PushEvent = ghEvent

		gh.SHA = *ghEvent.HeadCommit.ID
		gh.Branch = branchName(*ghEvent.Head)
	default:
		return nil, fmt.Errorf("received event of type %T that is not yet supported", ghEvent)
	}

	ct := BaseContainer(o.dag).
		WithEnvVariable("CACHE_BUST", time.Now().String()).
		WithMountedSecret("/root/.netrc", o.GithubNetrc)
	gh.Repository, gh.Changes, err = cloneAndDiff(ctx, ct, "https://github.com/"+gh.RepositoryName, gh.Branch, gh.SHA, baseRef, baseSha)
	if err != nil {
		return nil, fmt.Errorf("could not clond and diff repository: %s", err)
	}

	gh.Variables = map[string]string{
		"GITHUB_SHA":        gh.SHA,
		"GITHUB_ACTIONS":    "true",
		"GITHUB_EVENT_NAME": gh.EventType,
		"GITHUB_EVENT_PATH": "/raw-payload.json",
		"GITHUB_REF":        gh.Branch,
	}

	return gh, nil
}

func branchName(branch string) string {
	v := strings.TrimPrefix(branch, "refs/heads/")
	return strings.TrimPrefix(v, "refs/pull/")
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

func getDispatchModule(ctx context.Context, config *dagger.File) (string, error) {
	if config == nil {
		return ".", nil
	}

	contents, err := config.Contents(ctx)
	if err != nil {
		return "", err
	}

	spec := map[string]string{}
	if err := yaml.Unmarshal([]byte(contents), &spec); err != nil {
		return "", err
	}

	return spec["module-path"], nil
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
