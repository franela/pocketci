package main

import (
	"context"
	"dagger/dispatch-event/internal/dagger"
)

type DispatchEvent struct{}

func (m *DispatchEvent) Dispatch(ctx context.Context, vendor, event, filter string, src *dagger.Directory, eventTrigger *dagger.File) error {
	return nil
}

func (m *DispatchEvent) OnGithub(ctx context.Context, event, filter string, src *dagger.Directory, eventTrigger *dagger.File) error {
	return nil
}

func (m *DispatchEvent) OnGithubPullRequest(ctx context.Context, filter string, src *dagger.Directory, eventTrigger *dagger.File) error {
	return nil
}
