package pocketci

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"testing"

	"dagger.io/dagger"
	"gotest.tools/v3/assert"
)

func TestOrchestrator_HandleGithub(t *testing.T) {
	ctx := context.Background()
	client, err := dagger.Connect(ctx, dagger.WithLogOutput(os.Stderr))
	if err != nil {
		t.Fatalf("failed to connect to dagger: %v", err)
	}

	orchestrator := &Orchestrator{
		GithubNetrc: client.SetSecret("github_auth", fmt.Sprintf("machine github.com login %s password %s", os.Getenv("GH_USERNAME"), os.Getenv("GH_PASSWORD"))),
		Dispatcher: &LocalDispatcher{
			dag:         client,
			parallelism: 1,
		},
		dag: client,
	}

	cases := []struct {
		name    string
		webhook *Webhook
	}{
		{"pr opened", &Webhook{GithubVendor, GithubPullRequest, json.RawMessage(ghPrOpen)}},
		{"pr synced", &Webhook{GithubVendor, GithubPullRequest, json.RawMessage(ghPrSync)}},
		{"commit pushed", &Webhook{GithubVendor, GithubPush, json.RawMessage(ghCommitPush)}},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			assert.NilError(t, orchestrator.HandleGithub(ctx, test.webhook))
		})
	}
}

func TestParseRepositorySpec(t *testing.T) {
	ctx := context.Background()
	client, err := dagger.Connect(ctx, dagger.WithLogOutput(io.Discard))
	if err != nil {
		t.Fatalf("failed to connect to dagger: %v", err)
	}

	spec, err := parseRepositorySpec(ctx, client.Container().WithNewFile("/config.yaml", `module-path: ./dispatcher`).File("/config.yaml"))
	assert.NilError(t, err)
	assert.Equal(t, spec.ModulePath, "./dispatcher")
}
