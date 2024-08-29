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

var (
	//go:embed test-data/gh-pr-opened.json
	ghPrOpen []byte

	//go:embed test-data/gh-pr-sync.json
	ghPrSync []byte

	//go:embed test-data/gh-commit-push.json
	ghCommitPush []byte
)

func moduleBasePath() string {
	if os.Getenv("TRACEPARENT") != "" {
		return "./pocketci/test-data"
	}
	return "test-data"
}

func TestDispatcherFunction(t *testing.T) {
	ctx := context.Background()
	client, err := dagger.Connect(ctx, dagger.WithLogOutput(os.Stderr))
	if err != nil {
		t.Fatalf("failed to connect to dagger: %v", err)
	}

	cases := []struct {
		name                  string
		mod                   *dagger.Module
		vendor, event, filter string
		expectedFunctions     []function
	}{
		{
			name:              "filter gets matched",
			mod:               client.Host().Directory(moduleBasePath() + "/dispatch-filter").AsModule().Initialize(),
			vendor:            "github",
			event:             "pull_request",
			filter:            "opened",
			expectedFunctions: []function{{"on-github-pull-request-opened", ""}},
		},
		{
			name:              "event gets matched",
			mod:               client.Host().Directory(moduleBasePath() + "/dispatch-event").AsModule().Initialize(),
			vendor:            "github",
			event:             "pull_request",
			filter:            "opened",
			expectedFunctions: []function{{"on-github-pull-request", "--filter opened"}},
		},
		{
			name:              "vendor gets matched",
			mod:               client.Host().Directory(moduleBasePath() + "/dispatch-vendor").AsModule().Initialize(),
			vendor:            "github",
			event:             "pull_request",
			filter:            "opened",
			expectedFunctions: []function{{"on-github", "--filter opened --event pull_request"}},
		},
		{
			name:              "dispatch gets matched",
			mod:               client.Host().Directory(moduleBasePath() + "/dispatch").AsModule().Initialize(),
			vendor:            "github",
			event:             "pull_request",
			filter:            "opened",
			expectedFunctions: []function{{"dispatch", "--filter opened --event pull_request --vendor github"}},
		},
		{
			name:   "event with prefix gets matched",
			mod:    client.Host().Directory(moduleBasePath() + "/dispatch-suffix").AsModule().Initialize(),
			vendor: "github",
			event:  "pull_request",
			filter: "opened",
			expectedFunctions: []function{
				{"lint-on-github-pull-request", "--filter opened"},
				{"test-on-github-pull-request", "--filter opened"},
			},
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			fns, err := dispatcherFunction(ctx, "github", "pull_request", "opened", test.mod)
			assert.NilError(t, err)
			for i, fn := range fns {
				assert.Equal(t, fn, test.expectedFunctions[i])
			}
		})
	}
}

func TestShouldHandle(t *testing.T) {
	cases := []struct {
		name     string
		cfg      *Spec
		event    *Event
		expected bool
	}{
		{
			name:     "event does not exist",
			cfg:      &Spec{},
			event:    &Event{EventType: "nope"},
			expected: false,
		},
		{
			name:     "match pull_request",
			cfg:      &Spec{EventTrigger: EventTrigger{PullRequest: []string{}}},
			event:    &Event{EventType: "pull_request"},
			expected: true,
		},
		{
			name:     "match push",
			cfg:      &Spec{EventTrigger: EventTrigger{Push: []string{}}},
			event:    &Event{EventType: "push"},
			expected: true,
		},
		{
			name:     "match files",
			cfg:      &Spec{Paths: []string{"**/**.go"}},
			event:    &Event{EventType: "push", Changes: []string{"main.go"}},
			expected: true,
		},
		{
			name:     "does not match pull_request",
			cfg:      &Spec{EventTrigger: EventTrigger{PullRequest: []string{"main"}}},
			event:    &Event{EventType: "pull_request", BaseRef: "some"},
			expected: false,
		},
		{
			name:     "does not match push",
			cfg:      &Spec{EventTrigger: EventTrigger{Push: []string{"main"}}},
			event:    &Event{EventType: "push", Ref: "some"},
			expected: false,
		},
		{
			name:     "does not match files",
			cfg:      &Spec{Paths: []string{"**/**.go"}},
			event:    &Event{EventType: "push", Changes: []string{"README.md"}},
			expected: false,
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, shouldHandle(test.cfg, test.event), test.expected)
		})
	}
}

func TestAgent_GithubClone(t *testing.T) {
	ctx := context.Background()
	client, err := dagger.Connect(ctx, dagger.WithLogOutput(os.Stderr))
	if err != nil {
		t.Fatalf("failed to connect to dagger: %v", err)
	}

	agent := NewAgent(client)
	ghSecret := agent.CreateGithubSecret(os.Getenv("GH_USERNAME"), os.Getenv("GH_PASSWORD"))

	cases := []struct {
		name      string
		payload   json.RawMessage
		eventType string
	}{
		{"pr opened", ghPrOpen, "pull_request"},
		{"pr synced", ghPrSync, "pull_request"},
		{"commit pushed", ghCommitPush, "push"},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			event, err := agent.GithubClone(ctx, ghSecret, &GithubEvent{
				EventType: test.eventType,
				Payload:   test.payload,
			})
			assert.NilError(t, err)
			assert.Equal(t, event.RepoContents != nil, true)
		})
	}
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
