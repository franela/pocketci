package main

import (
	"context"
	"dagger/ci/internal/dagger"
)

func (m *Ci) Pipelines(ctx context.Context) *dagger.File {
	changes := []string{"**/**.go", "go.*"}
	branches := []string{"main"}

	checks := dag.Gha().WithPipeline("checks").
		WithOnChanges(changes).
		WithOnPullRequest([]dagger.GhaAction{dagger.Opened, dagger.Synchronize, dagger.Reopened}).
		WithOnPush(branches).
		WithModule("ci").
		Call("test & lint")

	publish := dag.Gha().WithPipeline("publish").
		WithOnChanges(changes).
		WithOnPush([]string{"main"}).
		WithModule("ci").
		Call("publish --sha env:COMMIT_SHA --username env:GH_USERNAME --password env:GH_PASSWORD").
		After([]*dagger.GhaPipeline{checks})

	return dag.Gha().Pipelines([]*dagger.GhaPipeline{checks, publish})
}
