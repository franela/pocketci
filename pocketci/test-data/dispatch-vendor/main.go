package main

import (
	"context"
	"dagger/dispatch-vendor/internal/dagger"
)

type DispatchVendor struct{}

func (m *DispatchVendor) Dispatch(ctx context.Context, vendor, event, filter string, src *dagger.Directory, eventTrigger *dagger.File) error {
	return nil
}

func (m *DispatchVendor) OnGithub(ctx context.Context, event, filter string, src *dagger.Directory, eventTrigger *dagger.File) error {
	return nil
}
