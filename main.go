package main

import (
	"context"
)

type Pocketci struct{}

// Starts the pocketci web handler
func (m *Pocketci) Serve(ctx context.Context, hooks Optional[*File], async Optional[bool]) (*Service, error) {
	goModCache := dag.CacheVolume("gomod")
	goBuildCache := dag.CacheVolume("gobuild")
	proxyFile := dag.Container().
		From("golang:1.21-alpine").
		WithDirectory("/app", dag.Host().Directory("proxy")).
		WithWorkdir("/app").
		WithEnvVariable("CGO_ENABLED", "0").
		WithMountedCache("/go/pkg/mod", goModCache).
		WithMountedCache("/root/.cache/go-build", goBuildCache).
		WithExec([]string{"go", "build", "-ldflags", "-s -w", "-o", "proxy", "."}).
		File("proxy")

	c := dag.
		Container().
		From("alpine").
		WithExposedPort(8080).
		WithFile("/proxy", proxyFile)

	exec := []string{"/proxy"}
	hooksFile, ok := hooks.Get()
	if ok {
		c = c.WithFile("/hooks.json", hooksFile)
		exec = append(exec, "-hooks", "/hooks.json")
	}

	if _, ok = async.Get(); ok {
		exec = append(exec, "-async", "true")
	}

	// we need nesting since the proxy uses dagger start webhook and
	return c.
		WithExec(exec, ContainerWithExecOpts{ExperimentalPrivilegedNesting: true}).
		AsService(), nil
}
