package main

import (
	"context"
	"dagger/dispatch-match-filter/internal/dagger"
)

type DispatchMatchFilter struct{}

func (m *DispatchMatchFilter) OnGithubPullRequest(ctx context.Context,
	// +default="synchronize,closed"
	filter string,
	src *dagger.Directory,
	eventTrigger *dagger.File) error {
	return nil
}
