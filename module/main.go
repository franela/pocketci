package main

import (
	"context"
	"net/url"

	"github.com/franela/webhook/internal/dagger"
)

type Pocketci struct{}

func (m *Pocketci) Publish(ctx context.Context, src *dagger.Directory, address, username string, password *dagger.Secret) (string, error) {
	u, err := url.Parse(address)
	if err != nil {
		return "", err
	}
	registry := u.Hostname()

	return m.BaseContainer(ctx, src).
		WithRegistryAuth(registry, username, password).
		Publish(ctx, address, dagger.ContainerPublishOpts{})
}

func (m *Pocketci) BaseContainer(ctx context.Context, src *dagger.Directory) *dagger.Container {
	goModCache := dag.CacheVolume("gomod")
	goBuildCache := dag.CacheVolume("gobuild")
	pocketci := dag.Container().
		From("golang:1.22-alpine").
		WithDirectory("/app", src).
		WithWorkdir("/app").
		WithEnvVariable("CGO_ENABLED", "0").
		WithMountedCache("/go/pkg/mod", goModCache).
		WithMountedCache("/root/.cache/go-build", goBuildCache).
		WithExec([]string{"go", "build", "-ldflags", "-s -w", "-o", "./bin/pocketci", "./cmd/agent"}).
		File("./bin/pocketci")

	return dag.
		Container().
		From("alpine:3.19").
		WithExposedPort(8080).
		WithFile("/pocketci", pocketci).
		WithWorkdir("/").
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
func (m *Pocketci) Serve(ctx context.Context, src *dagger.Directory,
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
