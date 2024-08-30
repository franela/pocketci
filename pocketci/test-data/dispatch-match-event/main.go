package main

import (
	"context"
	"dagger/dispatch-match-event/internal/dagger"
)

type DispatchMatchEvent struct{}

func (m *DispatchMatchEvent) OnGithub(ctx context.Context,
	// +default="push"
	event string,
	filter string,
	src *dagger.Directory,
	eventTrigger *dagger.File) error {
	return nil
}
