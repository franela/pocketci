package main

import (
	"context"
	"dagger/ci/internal/dagger"
)

func (m *Ci) Pipelines(ctx context.Context) *dagger.File {
	// call hello for testing purposes
	dag.Hello().Hello(ctx)
	changes := []string{"**/**.go", "go.*"}
	branches := []string{"main"}

	checks := dag.Gha().Pipeline("checks").
		OnChanges(changes).
		OnPullRequest([]dagger.GhaAction{dagger.GhaActionOpened, dagger.GhaActionSynchronize, dagger.GhaActionReopened}).
		OnPush(branches).
		Module("ci").
		Call("test & lint")

	publish := dag.Gha().Pipeline("publish").
		OnChanges(changes).
		OnPush([]string{"main"}).
		Module("ci").
		Call("publish --sha env:COMMIT_SHA --username env:GH_USERNAME --password env:GH_PASSWORD").
		After([]*dagger.GhaPipeline{checks})

	return dag.Gha().Pipelines([]*dagger.GhaPipeline{checks, publish})
}
