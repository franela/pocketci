package main

import (
	"context"
	"dagger/dispatch-suffix/internal/dagger"
)

type DispatchSuffix struct{}

func (m *DispatchSuffix) Dispatch(ctx context.Context, vendor, event, filter string, src *dagger.Directory, eventTrigger *dagger.File) error {
	return nil
}

func (m *DispatchSuffix) OnGithub(ctx context.Context, event, filter string, src *dagger.Directory, eventTrigger *dagger.File) error {
	return nil
}

func (m *DispatchSuffix) LintOnGithubPullRequest(ctx context.Context, filter string, src *dagger.Directory, eventTrigger *dagger.File) error {
	return nil
}

func (m *DispatchSuffix) TestOnGithubPullRequest(ctx context.Context, filter string, src *dagger.Directory, eventTrigger *dagger.File) error {
	return nil
}
