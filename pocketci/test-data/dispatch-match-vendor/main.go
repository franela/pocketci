package main

import (
	"context"
	"dagger/dispatch-match-vendor/internal/dagger"
)

type DispatchMatchVendor struct{}

func (m *DispatchMatchVendor) Dispatch(ctx context.Context,
	// +default="gitlab"
	vendor string,
	event string,
	filter string,
	src *dagger.Directory,
	eventTrigger *dagger.File) error {
	return nil
}
