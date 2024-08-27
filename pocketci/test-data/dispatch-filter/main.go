package main

import (
	"context"
	"dagger/dispatch-filter/internal/dagger"
)

type DispatchFilter struct{}

func (m *DispatchFilter) Dispatch(ctx context.Context, vendor, event, filter string, src *dagger.Directory, eventTrigger *dagger.File) error {
	return nil
}

func (m *DispatchFilter) OnGithub(ctx context.Context, event, filter string, src *dagger.Directory, eventTrigger *dagger.File) error {
	return nil
}

func (m *DispatchFilter) OnGithubPullRequest(ctx context.Context, filter string, src *dagger.Directory, eventTrigger *dagger.File) error {
	return nil
}

func (m *DispatchFilter) OnGithubPullRequestOpened(ctx context.Context, src *dagger.Directory, eventTrigger *dagger.File) error {
	return nil
}
