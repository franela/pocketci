package pocketci

import (
	"context"
	_ "embed"
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

func TestMatchFunctions(t *testing.T) {
	t.Skip()

	ctx := context.Background()
	client, err := dagger.Connect(ctx, dagger.WithLogOutput(os.Stderr))
	if err != nil {
		t.Fatalf("failed to connect to dagger: %v", err)
	}

	cases := []struct {
		name                  string
		mod                   *dagger.Module
		vendor, event, filter string
		changes               []string
		expectedFunctions     []Function
	}{
		{
			name:              "filter gets matched",
			mod:               client.Host().Directory(moduleBasePath() + "/dispatch-filter").AsModule().Initialize(),
			vendor:            "github",
			event:             "pull_request",
			filter:            "opened",
			expectedFunctions: []Function{{"on-github-pull-request-opened", ""}},
		},
		{
			name:              "event gets matched",
			mod:               client.Host().Directory(moduleBasePath() + "/dispatch-event").AsModule().Initialize(),
			vendor:            "github",
			event:             "pull_request",
			filter:            "opened",
			expectedFunctions: []Function{{"on-github-pull-request", "--filter opened"}},
		},
		{
			name:              "vendor gets matched",
			mod:               client.Host().Directory(moduleBasePath() + "/dispatch-vendor").AsModule().Initialize(),
			vendor:            "github",
			event:             "pull_request",
			filter:            "opened",
			expectedFunctions: []Function{{"on-github", "--filter opened --event pull_request"}},
		},
		{
			name:              "dispatch gets matched",
			mod:               client.Host().Directory(moduleBasePath() + "/dispatch").AsModule().Initialize(),
			vendor:            "github",
			event:             "pull_request",
			filter:            "opened",
			expectedFunctions: []Function{{"dispatch", "--filter opened --event pull_request --vendor github"}},
		},
		{
			name:   "event with prefix gets matched",
			mod:    client.Host().Directory(moduleBasePath() + "/dispatch-suffix").AsModule().Initialize(),
			vendor: "github",
			event:  "pull_request",
			filter: "opened",
			expectedFunctions: []Function{
				{"lint-on-github-pull-request", "--filter opened"},
				{"test-on-github-pull-request", "--filter opened"},
			},
		},
		{
			name:              "no functions match",
			mod:               client.Host().Directory(moduleBasePath() + "/dispatch-none").AsModule().Initialize(),
			vendor:            "github",
			event:             "pull_request",
			filter:            "opened",
			expectedFunctions: []Function{},
		},
		{
			name:              "match by files changed",
			mod:               client.Host().Directory(moduleBasePath() + "/dispatch-changed-files").AsModule().Initialize(),
			vendor:            "github",
			event:             "pull_request",
			filter:            "opened",
			changes:           []string{"main.go", "go.mod"},
			expectedFunctions: []Function{{"lint-on-github-pull-request", "--filter opened --on-changes main.go,go.mod"}},
		},
		{
			name:              "no functions because vendor is not matched",
			mod:               client.Host().Directory(moduleBasePath() + "/dispatch-match-vendor").AsModule().Initialize(),
			vendor:            "github",
			event:             "pull_request",
			filter:            "opened",
			expectedFunctions: []Function{},
		},
		{
			name:              "no functions because filter is not matched",
			mod:               client.Host().Directory(moduleBasePath() + "/dispatch-match-filter").AsModule().Initialize(),
			vendor:            "github",
			event:             "pull_request",
			filter:            "opened",
			expectedFunctions: []Function{},
		},
		{
			name:              "no functions because event is not matched",
			mod:               client.Host().Directory(moduleBasePath() + "/dispatch-match-event").AsModule().Initialize(),
			vendor:            "github",
			event:             "pull_request",
			filter:            "opened",
			expectedFunctions: []Function{},
		},
		{
			name:              "vendor matches via field defaultValue",
			mod:               client.Host().Directory(moduleBasePath() + "/dispatch-match-vendor").AsModule().Initialize(),
			vendor:            "gitlab",
			event:             "push",
			filter:            "main",
			expectedFunctions: []Function{{"dispatch", "--filter main --event push --vendor gitlab"}},
		},
		{
			name:              "filter matches via field defaultValue",
			mod:               client.Host().Directory(moduleBasePath() + "/dispatch-match-filter").AsModule().Initialize(),
			vendor:            "github",
			event:             "pull_request",
			filter:            "synchronize",
			expectedFunctions: []Function{{"on-github-pull-request", "--filter synchronize"}},
		},
		{
			name:              "event matches via field defaultValue",
			mod:               client.Host().Directory(moduleBasePath() + "/dispatch-match-event").AsModule().Initialize(),
			vendor:            "github",
			event:             "push",
			filter:            "main",
			expectedFunctions: []Function{{"on-github", "--filter main --event push"}},
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			functions, err := matchFunctions(ctx, test.vendor, test.event, test.filter, test.changes, test.mod)
			if len(test.expectedFunctions) == 0 {
				assert.ErrorIs(t, err, ErrNoFunctionsMatched)
				return
			}

			assert.NilError(t, err)
			assert.Equal(t, len(functions), len(test.expectedFunctions))
			for i, fn := range functions {
				assert.Equal(t, fn, test.expectedFunctions[i])
			}
		})
	}
}
