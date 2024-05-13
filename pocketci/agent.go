package pocketci

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"dagger.io/dagger"
	"dagger.io/dagger/dag"

	"github.com/google/go-github/v61/github"
)

var (
	ErrPushEventIgnored = errors.New("push event ignored due to duplication")
)

const DaggerVersion = "0.11.3"

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

func (agent *Agent) GithubClone(ctx context.Context, netrc *dagger.Secret, githubEvent *GithubEvent) (*dagger.Directory, string, error) {
	event, err := github.ParseWebHook(githubEvent.EventType, githubEvent.Payload)
	if err != nil {
		return nil, "", err
	}

	var (
		gitSha     string
		repository string
		ref        string
	)
	switch ghEvent := event.(type) {
	case *github.PullRequestEvent:
		gitSha = *ghEvent.PullRequest.Head.SHA
		repository = *ghEvent.Repo.FullName
		ref = *ghEvent.PullRequest.Head.Ref
	case *github.PushEvent:
		// NOTE: If users have `PushEvent` enabled in their lists of webhooks
		// then we receive duplicated events every time a commit is pushed to
		// a pull request. To simplify how pocketci works we need to choose
		// to handle only one of those events when this duplication happens.
		// The easiest way of doing this is to ignore all push events that are not
		// on the typical main branches (develop, main, master, trunk). This will
		// prevent users from creating workflows that are triggered based on commits
		// that happen against arbitrary branches. There are workarounds we can apply
		// but they will complicate the implementation and I would rather wait
		// until people request the feature
		gitSha = *ghEvent.After
		repository = *ghEvent.Repo.FullName
		ref = *ghEvent.Ref
		if ref != "main" && ref != "master" && ref != "develop" && ref != "trunk" {
			return nil, "", ErrPushEventIgnored
		}
	}

	fullRepo := strings.Split(repository, "/")
	repo := fullRepo[len(fullRepo)-1]

	// NOTE: it is important that we check out the repository with at least some
	// history. We need at least two commits (or just one if its the initial commit)
	// in order to compute the list of changes of the latest commit. We use
	// a manual git clone instead of dagger's builtin dag.Git function because
	// of this requirement.
	dir, err := BaseContainer(agent.dag).
		WithEnvVariable("CACHE_BUST", time.Now().String()).
		WithMountedSecret("/root/.netrc", netrc).
		WithExec([]string{"git", "clone", "--single-branch", "--branch", ref, "--depth", "10", "https://github.com/" + repository}).
		WithWorkdir("/" + repo).
		WithExec([]string{"git", "checkout", gitSha}).
		Directory("/" + repo).
		Sync(ctx)
	if err != nil {
		return nil, "", err
	}
	return dir, repo, nil
}

func (agent *Agent) HandleGithub(ctx context.Context, netrc *dagger.Secret, event *GithubEvent) (*dagger.Service, error) {
	repoDir, repo, err := agent.GithubClone(ctx, netrc, event)
	if err != nil {
		return nil, err
	}

	ct := WebhookContainer(agent.dag).
		WithEnvVariable("CACHE_BUST", time.Now().String()).
		WithDirectory("/"+repo, repoDir).
		WithWorkdir("/" + repo)

	filesChanged, err := ct.
		WithExec([]string{"git", "diff-tree", "--no-commit-id", "--name-only", "-r", "HEAD"}).
		Stdout(ctx)
	if err != nil {
		return nil, err
	}
	event.Changes = strings.Split(strings.TrimSuffix(filesChanged, "\n"), "\n")

	payload, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}

	secrets, err := agent.parseSecretParameters(ctx, repoDir.File("dispatcher/dagger.json"))
	if err != nil {
		return nil, err
	}

	return ct.
		WithNewFile("/payload.json", dagger.ContainerWithNewFileOpts{
			Contents: string(payload),
		}).
		WithFile(fmt.Sprintf("/%s/hooks.yaml", repo), agent.hooksFile(ctx, repo, os.Getenv("X_HUB_SIGNATURE"), secrets)).
		With(func(c *dagger.Container) *dagger.Container {
			for _, secret := range secrets {
				c = c.WithSecretVariable(secret.EnvVariable, agent.dag.SetSecret(secret.ParameterName, os.Getenv(secret.EnvVariable)))
			}
			return c
		}).
		WithExposedPort(9000).
		WithExec([]string{"/usr/local/bin/webhook", "-verbose", "-port", "9000", "-hooks", "hooks.yaml"}, dagger.ContainerWithExecOpts{ExperimentalPrivilegedNesting: true}).
		AsService(), nil
}

type DaggerConfig struct {
	Secrets []string `json:"secrets"`
}

type DispatchSecret struct {
	ParameterName string
	EnvVariable   string
}

func (agent *Agent) parseSecretParameters(ctx context.Context, config *dagger.File) ([]DispatchSecret, error) {
	file, err := config.Contents(ctx)
	if err != nil {
		return nil, err
	}

	var cfg DaggerConfig
	if err := json.Unmarshal([]byte(file), &cfg); err != nil {
		return nil, err
	}

	if len(cfg.Secrets) == 0 {
		return []DispatchSecret{}, nil
	}

	secrets := []DispatchSecret{}
	for _, secret := range cfg.Secrets {
		split := strings.Split(secret, ":")
		if len(split) != 2 {
			return nil, fmt.Errorf("invalid secret: expected `parameterName:ENV_VARIABLE_NAME` got %s", secret)
		}
		secrets = append(secrets, DispatchSecret{ParameterName: split[0], EnvVariable: split[1]})
	}

	return secrets, nil
}

func (agent *Agent) hooksFile(ctx context.Context, repo, xHubSignature string, secrets []DispatchSecret) *dagger.File {
	var encodedSecrets string
	for _, secret := range secrets {
		encodedSecrets = fmt.Sprintf(`%s
  - source: string
    name: "--%s"
  - source: string
    name: "env:%s"`, encodedSecrets, secret.ParameterName, secret.EnvVariable)
	}

	return dag.Container().
		WithNewFile("/hooks.yaml", dagger.ContainerWithNewFileOpts{
			Contents: fmt.Sprintf(`- id: git-push
  execute-command: "/bin/dagger"
  include-command-output-in-response: true
  command-working-directory: "/%s"
  pass-arguments-to-command:
  - source: string
    name: call
  - source: string
    name: "--mod"
  - source: string
    name: "./dispatcher"
  - source: string
    name: "--progress"
  - source: string
    name: "plain"
  - source: string
    name: "dispatch"
  - source: string
    name: "--src"
  - source: string
    name: "."
  - source: string
    name: "--payload"
  - source: string
    name: "/payload.json"
%s
  trigger-rule:
    and:
    - match:
        type: payload-hmac-sha1
        secret: %s
        parameter:
          source: header
          name: X-Hub-Signature`, repo, encodedSecrets, xHubSignature)}).
		File("/hooks.yaml")
}

func BaseContainer(c *dagger.Client) *dagger.Container {
	return c.Container().From("ubuntu:lunar").
		WithExec([]string{"sh", "-c", "apt update && apt install -y curl wget git"})
}

func WebhookContainer(c *dagger.Client) *dagger.Container {
	return BaseContainer(c).
		WithExec([]string{"wget", "-q", "https://github.com/adnanh/webhook/releases/download/2.8.1/webhook-linux-amd64.tar.gz"}).
		WithExec([]string{"tar", "-C", "/usr/local/bin", "--strip-components", "1", "-xf", "webhook-linux-amd64.tar.gz", "webhook-linux-amd64/webhook"}).
		WithExec([]string{"sh", "-c", fmt.Sprintf(`cd / && DAGGER_VERSION="%s" curl -L https://dl.dagger.io/dagger/install.sh | DAGGER_VERSION="%s" sh`, DaggerVersion, DaggerVersion)})
}
