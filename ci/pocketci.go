package main

import (
	"context"
	"dagger/ci/internal/dagger"
)

func (m *Ci) TestOnGithubPullRequest(ctx context.Context,
	// +optional
	// +default="**/**.go,go.*"
	onChanges string,
	// +default="synchronize,opened,reopened"
	filter string,
	src *dagger.Directory,
	eventTrigger *dagger.File,
	ghUsername, ghPassword *dagger.Secret) error {
	_, err := m.Test(ctx, src, ghUsername, ghPassword).Stdout(ctx)
	return err
}

func (m *Ci) LintOnGithubPullRequest(ctx context.Context,
	// +optional
	// +default="**/**.go,go.*"
	onChanges string,
	// +default="synchronize,opened,reopened"
	filter string,
	src *dagger.Directory,
	eventTrigger *dagger.File,
	ghUsername, ghPassword *dagger.Secret) error {
	_, err := m.Lint(ctx, src).Stdout(ctx)
	return err
}

func (m *Ci) PublishOnGithubPush(ctx context.Context,
	// +optional
	// +default="**/**.go,go.*"
	onChanges string,
	// +default="main"
	filter string,
	src *dagger.Directory,
	eventTrigger *dagger.File,
	ghUsername, ghPassword *dagger.Secret) error {

	sha, err := dag.Pocketci(eventTrigger).CommitPush().Sha(ctx)
	if err != nil {
		return err
	}

	username, _ := ghUsername.Plaintext(ctx)
	_, err = m.Publish(ctx, src, sha, username, ghPassword)
	return err
}

func (m *Ci) TestOnGithubPushMain(ctx context.Context,
	// +optional
	// +default="**/**.go,go.*"
	onChanges string,
	src *dagger.Directory,
	eventTrigger *dagger.File,
	ghUsername, ghPassword *dagger.Secret) error {
	_, err := m.Test(ctx, src, ghUsername, ghPassword).Stdout(ctx)
	return err
}

func (m *Ci) LintOnGithubPushMain(ctx context.Context,
	// +optional
	// +default="**/**.go,go.*"
	onChanges string,
	src *dagger.Directory,
	eventTrigger *dagger.File,
	ghUsername, ghPassword *dagger.Secret) error {
	_, err := m.Lint(ctx, src).Stdout(ctx)
	return err
}
