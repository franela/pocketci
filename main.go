package main

import (
	"context"
	"fmt"
)

type Webhook struct{}

func (m *Webhook) Proxy(ctx context.Context, proxyPort int, server *Directory, repoName string) *Service {
	repoDir := dag.CacheVolume(repoName)
	return dag.
		Container().
		From("golang:1.21-alpine").
		WithExec([]string{"apk", "update"}).
		WithExec([]string{"apk", "add", "git"}).
		WithDirectory("/server", server).
		WithMountedCache("/"+repoName, repoDir).
		WithExec([]string{"touch", "/" + repoName + "/hooks.json"}).
		WithServiceBinding("webhook", m.webhookService(ctx, 9000, repoName, repoDir)).
		WithExposedPort(proxyPort).
		WithExec([]string{"go", "run", "/server/main.go",
			"-port", fmt.Sprintf("%d", proxyPort), "-repo-name", repoName, "-server", "http://webhook:9000"}).
		AsService()
}

func (m *Webhook) webhookService(ctx context.Context, serverPort int, repoName string, repoDir *CacheVolume) *Service {
	return dag.
		Container().
		From("ghcr.io/matipan/webhook:dagger-1").
		WithMountedCache("/"+repoName, repoDir).
		WithExposedPort(serverPort).
		WithExec([]string{"/webhook", "-hooks", "/" + repoName + "/hooks.json", "-verbose", "-port", fmt.Sprintf("%d", serverPort)},
			ContainerWithExecOpts{ExperimentalPrivilegedNesting: true}).
		AsService()
}
