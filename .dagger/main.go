package main

import (
	"context"
	"dagger/pocketci/internal/dagger"
)

type Pocketci struct{}

func (m *Pocketci) Pipelines(ctx context.Context) *dagger.File {
	changes := []string{"**/**.go", "go.*"}
	branches := []string{"main"}

	checks := dag.Gha().Pipeline("checks").
		OnChanges(changes).
		OnPullRequest([]dagger.GhaAction{dagger.Opened, dagger.Synchronize, dagger.Reopened}).
		OnPush(branches).
		Module("ci").
		Call("test").
		Call("lint")

	publish := dag.Gha().Pipeline("publish").
		OnChanges(changes).
		OnPush([]string{"main"}).
		Module("ci").
		Call("publish --sha env:COMMIT_SHA --username env:GH_USERNAME --password env:GH_PASSWORD").
		After([]*dagger.GhaPipeline{checks})

	return dag.Gha().Pipelines([]*dagger.GhaPipeline{checks, publish})
}
