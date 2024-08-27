package main

import (
	"context"
	"dagger/dispatch/internal/dagger"
)

type Dispatch struct{}

func (m *Dispatch) Dispatch(ctx context.Context, vendor, event, filter string, src *dagger.Directory, eventTrigger *dagger.File) error {
	return nil
}
