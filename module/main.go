package main

import (
	"context"
	"net/url"
)

type Pocketci struct{}

func (m *Pocketci) Publish(ctx context.Context, src *Directory, address, username string, password *Secret) (string, error) {
	u, err := url.Parse(address)
	if err != nil {
		return "", err
	}
	registry := u.Hostname()

	return m.BaseContainer(ctx, src).
		WithRegistryAuth(registry, username, password).
		Publish(ctx, address, ContainerPublishOpts{})
}

func (m *Pocketci) BaseContainer(ctx context.Context, src *Directory) *Container {
	goModCache := dag.CacheVolume("gomod")
	goBuildCache := dag.CacheVolume("gobuild")
	pocketci := dag.Container().
		From("golang:1.21-alpine").
		WithDirectory("/app", src).
		WithWorkdir("/app").
		WithEnvVariable("CGO_ENABLED", "0").
		WithMountedCache("/go/pkg/mod", goModCache).
		WithMountedCache("/root/.cache/go-build", goBuildCache).
		WithExec([]string{"go", "build", "-ldflags", "-s -w", "-o", "pocketci", "./proxy"}).
		File("pocketci")

	return dag.
		Container().
		From("alpine:3.19").
		WithExposedPort(8080).
		WithFile("/pocketci", pocketci).
		WithWorkdir("/").
		WithExec([]string{"apk", "add", "--update", "--no-cache", "ca-certificates", "curl", "docker", "openrc"}).
		WithExec([]string{"curl", "-LO", "https://github.com/dagger/dagger/releases/download/v0.9.7/dagger_v0.9.7_linux_amd64.tar.gz"}).
		WithExec([]string{"tar", "xvf", "dagger_v0.9.7_linux_amd64.tar.gz"}).
		WithExec([]string{"mv", "dagger", "/bin/dagger"}).
		WithExec([]string{"rm", "dagger_v0.9.7_linux_amd64.tar.gz", "LICENSE"}).
		WithEntrypoint([]string{"/pocketci"})
}

// Starts the pocketci web handler
func (m *Pocketci) Serve(ctx context.Context, src *Directory, hooks Optional[*File], async Optional[bool]) (*Service, error) {
	c := m.BaseContainer(ctx, src)

	exec := []string{}
	hooksFile, ok := hooks.Get()
	if ok {
		c = c.WithFile("/hooks.yaml", hooksFile)
		exec = append(exec, "-hooks", "/hooks.yaml")
	}

	if _, ok = async.Get(); ok {
		exec = append(exec, "-async", "true")
	}

	// we need nesting since the proxy uses dagger start webhook and
	return c.
		WithExec(exec, ContainerWithExecOpts{ExperimentalPrivilegedNesting: true}).
		AsService(), nil
}
