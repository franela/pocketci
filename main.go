package main

import (
	"context"
)

type Webhook struct{}

func (m *Webhook) Webhook(ctx context.Context) (*Service, error) {
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

	return dag.
		Container().
		From("alpine").
		WithExposedPort(8080).
		WithFile("/proxy", proxyFile).
		// we need nesting since the proxy uses dagger start webhook and
		WithExec([]string{"/proxy"}, ContainerWithExecOpts{ExperimentalPrivilegedNesting: true}).
		AsService(), nil
}
