package pocketci

import (
	"context"
	_ "embed"
	"encoding/json"
	"io"
	"os"
	"testing"

	"dagger.io/dagger"
	"gotest.tools/v3/assert"
)

//go:embed test-data/gh-pr-webhook.json
var ghPrWebhook []byte

func TestAgent_GithubCloneFromPullRequest(t *testing.T) {
	ctx := context.Background()
	client, err := dagger.Connect(ctx, dagger.WithLogOutput(os.Stderr))
	if err != nil {
		t.Fatalf("failed to connect to dagger: %v", err)
	}

	agent := NewAgent(client)

	ghSecret := agent.CreateGithubSecret(os.Getenv("GH_USERNAME"), os.Getenv("GH_PASSWORD"))

	event, err := agent.GithubClone(ctx, ghSecret, &GithubEvent{
		EventType: "pull_request",
		Payload:   json.RawMessage(ghPrWebhook),
	})
	assert.NilError(t, err)
	assert.Equal(t, event.RepoContents != nil, true)
}

func TestParsePocketciConfig(t *testing.T) {
	ctx := context.Background()
	client, err := dagger.Connect(ctx, dagger.WithLogOutput(io.Discard))
	if err != nil {
		t.Fatalf("failed to connect to dagger: %v", err)
	}

	spec, err := parsePocketciConfig(ctx, client.Container().WithNewFile("/config.yaml", `module-path: ./dispatcher`).File("/config.yaml"))
	assert.NilError(t, err)
	assert.Equal(t, spec.ModulePath, "./dispatcher")
}
