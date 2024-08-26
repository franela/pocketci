package main

import (
	"context"

	"dagger/ci/internal/dagger"
)

type Ci struct{}

func (m *Ci) Publish(ctx context.Context, src *dagger.Directory, tag, username string, password *dagger.Secret) (string, error) {
	return m.BaseContainer(ctx, src).
		WithRegistryAuth("ghcr.io", username, password).
		Publish(ctx, "ghcr.io/franela/pocketci:"+tag)
}

func (m *Ci) OnPullRequest(ctx context.Context, src *dagger.Directory, eventTrigger *dagger.File, ghUsername, ghPassword *dagger.Secret) error {
	_, err := m.Test(ctx, src, ghUsername, ghPassword).Stdout(ctx)
	return err
}

func (m *Ci) OnCommitPush(ctx context.Context, src *dagger.Directory, eventTrigger *dagger.File, ghUsername, ghPassword *dagger.Secret) error {
	sha, err := dag.Pocketci(eventTrigger).CommitPush().Sha(ctx)
	if err != nil {
		return err
	}

	username, _ := ghUsername.Plaintext(ctx)
	_, err = m.Publish(ctx, src, sha, username, ghPassword)
	return err
}

/*func (m *Ci) Dispatch(ctx context.Context, src *dagger.Directory, eventTrigger *dagger.File, ghUsername, ghPassword *dagger.Secret) error {
	ci := dag.Pocketci(eventTrigger)
	event, err := ci.EventType(ctx)
	if err != nil {
		return err
	}

	switch event {
	case dagger.PullRequest:
		_, err := m.Test(ctx, src, ghUsername, ghPassword).Stdout(ctx)
		return err
	case dagger.Push:
		sha, err := ci.CommitPush().Sha(ctx)
		if err != nil {
			return err
		}
		username, _ := ghUsername.Plaintext(ctx)
		_, err = m.Publish(ctx, src, sha, username, ghPassword)
		return err
	}

	return nil
}*/

func (m *Ci) Test(ctx context.Context, src *dagger.Directory, ghUsername, ghPassword *dagger.Secret) *dagger.Container {
	return m.base(src).
		WithSecretVariable("GH_USERNAME", ghUsername).
		WithSecretVariable("GH_PASSWORD", ghPassword).
		WithExec([]string{"sh", "-c", "go test -v ./pocketci/..."}, dagger.ContainerWithExecOpts{ExperimentalPrivilegedNesting: true})
}

func (m *Ci) base(src *dagger.Directory) *dagger.Container {
	goModCache := dag.CacheVolume("gomod")
	goBuildCache := dag.CacheVolume("gobuild")
	return dag.Container().
		From("golang:1.22-alpine").
		WithDirectory("/app", src).
		WithWorkdir("/app").
		WithEnvVariable("CGO_ENABLED", "0").
		WithMountedCache("/go/pkg/mod", goModCache).
		WithMountedCache("/root/.cache/go-build", goBuildCache)
}

func (m *Ci) BaseContainer(ctx context.Context, src *dagger.Directory) *dagger.Container {
	pocketci := m.base(src).
		WithExec([]string{"go", "build", "-ldflags", "-s -w", "-o", "./bin/pocketci", "./cmd/agent"}).
		File("./bin/pocketci")

	return dag.
		Container().
		From("alpine:3.19").
		WithExposedPort(8080).
		WithFile("/pocketci", pocketci).
		WithExec([]string{"apk", "add", "--update", "--no-cache", "docker", "openrc"}).
		WithFile(
			"dagger.tgz",
			dag.HTTP("https://github.com/dagger/dagger/releases/download/v0.11.9/dagger_v0.11.9_linux_amd64.tar.gz"),
		).
		WithExec([]string{"tar", "xvf", "dagger.tgz"}).
		WithExec([]string{"mv", "dagger", "/bin/dagger"}).
		WithoutFile("dagger.tgz").
		WithoutFile("LICENSE").
		WithEntrypoint([]string{"/pocketci"})
}

// Starts the pocketci web handler
func (m *Ci) Serve(ctx context.Context, src *dagger.Directory,
	// +optional
	hooks *dagger.File,
	// +optional
	async bool) (*dagger.Service, error) {
	c := m.BaseContainer(ctx, src)

	exec := []string{"/pocketci"}
	if hooks != nil {
		c = c.WithFile("/hooks.yaml", hooks)
		exec = append(exec, "-hooks", "/hooks.yaml")
	}

	if async {
		exec = append(exec, "-async", "true")
	}

	// we need nesting since the proxy uses dagger start webhook and
	return c.
		WithoutEntrypoint(). // to get same behavior in 0.11 and 0.12
		WithExec(exec, dagger.ContainerWithExecOpts{ExperimentalPrivilegedNesting: true}).
		AsService(), nil
}
