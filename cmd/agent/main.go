package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"

	"dagger.io/dagger"
	"github.com/franela/pocketci/pocketci"
)

var (
	controlPlane = flag.String("control-plane", "", "url to control plane host")
	interval     = flag.Duration("interval", 5*time.Second, "interval between pipeline polls")
	runnerName   = flag.String("runner-name", "", "name of the runner that identifies it")
	parallelism  = flag.Int("parallelism", 10, "max number of dagger calls to run in parallel")

	ErrNoPipeline = errors.New("no pipeline to run")
)

func main() {
	flag.Parse()

	if *controlPlane == "" {
		log.Fatalf("control-plane must be specified and be a valid url")
	}
	if *runnerName == "" {
		log.Fatalf("runner-name must be specified")
	}

	ctx := context.Background()

	client, err := dagger.Connect(ctx, dagger.WithLogOutput(os.Stderr))
	if err != nil {
		log.Fatalf("failed to connect to dagger client: %s", err)
	}

	mu := make(chan bool, *parallelism)
	for i := 0; i < *parallelism; i++ {
		mu <- true
	}

	githubUser := os.Getenv("GITHUB_USERNAME")
	githubPass := os.Getenv("GITHUB_TOKEN")
	netrc := client.SetSecret("github_auth", fmt.Sprintf("machine github.com login %s password %s", githubUser, githubPass))

	for {
		pipeline, err := getPipeline(ctx)
		if err != nil && !errors.Is(err, ErrNoPipeline) {
			log.Fatalf("failed to fetch pipeline: %s", err)
		}

		if errors.Is(err, ErrNoPipeline) {
			slog.Info("no pipeline to run")
			time.Sleep(*interval)
			continue
		}

		go func() {
			// wait for parallelism
			<-mu
			defer func() {
				mu <- true
			}()

			run(ctx, client, netrc, pipeline)
			pipelineDone(pipeline)
		}()

		time.Sleep(*interval)
	}
}

func pipelineDone(pipeline *pocketci.PocketciPipeline) {
	res, err := http.Post(*controlPlane+"/pipelines/"+strconv.Itoa(pipeline.ID), "application/json", nil)
	if err != nil {
		slog.Error("could not mark pipeline as done", slog.String("error", err.Error()))
		return
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusNoContent {
		slog.Error("could not mark pipeline as done", slog.Int("status_code", res.StatusCode))
	}
	slog.Info("pipeline is done", slog.Int("pipeline", pipeline.ID))
}

func getPipeline(ctx context.Context) (*pocketci.PocketciPipeline, error) {
	buf := bytes.NewBuffer([]byte{})
	if err := json.NewEncoder(buf).Encode(pocketci.PipelineClaimRequest{RunnerName: *runnerName}); err != nil {
		return nil, err
	}

	res, err := http.Post(*controlPlane+"/pipelines/claim", "application/json", buf)
	if err != nil {
		return nil, err
	}
	if res.StatusCode == http.StatusNoContent {
		return nil, ErrNoPipeline
	}

	pipeline := &pocketci.PocketciPipeline{}
	if err := json.NewDecoder(res.Body).Decode(pipeline); err != nil {
		return nil, err
	}

	return pipeline, nil
}

func run(ctx context.Context, dag *dagger.Client, netrc *dagger.Secret, req *pocketci.PocketciPipeline) {
	repoUrl := "https://github.com/" + req.Repository
	slog.Info("cloning repository", slog.String("repository", repoUrl),
		slog.String("ref", req.GitInfo.Branch), slog.String("sha", req.GitInfo.SHA))

	repo, err := pocketci.BaseContainer(dag).
		WithEnvVariable("CACHE_BUST", time.Now().String()).
		WithMountedSecret("/root/.netrc", netrc).
		WithExec([]string{"git", "clone", "--single-branch", "--branch", req.GitInfo.Branch, "--depth", "1", repoUrl, "/app"}).
		WithWorkdir("/app").
		WithExec([]string{"git", "checkout", req.GitInfo.SHA}).
		Directory("/app").
		Sync(ctx)
	if err != nil {
		slog.Error("failed to clonse github repository", slog.String("error", err.Error()),
			slog.String("repository", repoUrl), slog.String("ref", req.GitInfo.Branch), slog.String("sha", req.GitInfo.SHA))
		return
	}

	vars := map[string]string{
		"GITHUB_SHA":     req.GitInfo.SHA,
		"GITHUB_ACTIONS": "true",
	}

	slog.Info("launching pocketci agent container",
		slog.String("repository_name", req.Repository), slog.String("pipeline", req.Name),
		slog.String("ref", req.GitInfo.Branch), slog.String("sha", req.GitInfo.SHA),
		slog.String("module", req.Module), slog.String("exec", req.Call),
		slog.String("runs_on", req.Runner))

	call := fmt.Sprintf("dagger call -m ci --progress plain %s", req.Call)
	if req.Module != "" {
		call = fmt.Sprintf("dagger call -m %s --progress plain %s", req.Module, req.Call)
	}
	stdout, err := pocketci.AgentContainer(dag).
		WithEnvVariable("CACHE_BUST", time.Now().String()).
		WithEnvVariable("DAGGER_CLOUD_TOKEN", os.Getenv("DAGGER_CLOUD_TOKEN")).
		WithDirectory("/app", repo).
		WithWorkdir("/app").
		WithEnvVariable("CI", "pocketci").
		With(func(c *dagger.Container) *dagger.Container {
			for key, val := range vars {
				c = c.WithEnvVariable(key, val)
			}
			script := fmt.Sprintf("unset TRACEPARENT;unset OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf;unset OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:38015;unset OTEL_EXPORTER_OTLP_TRACES_PROTOCOL=http/protobuf;unset OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=http://127.0.0.1:38015/v1/traces;unset OTEL_EXPORTER_OTLP_TRACES_LIVE=1;unset OTEL_EXPORTER_OTLP_LOGS_PROTOCOL=http/protobuf;unset OTEL_EXPORTER_OTLP_LOGS_ENDPOINT=http://127.0.0.1:38015/v1/logs;unset OTEL_EXPORTER_OTLP_METRICS_PROTOCOL=http/protobuf;unset OTEL_EXPORTER_OTLP_METRICS_ENDPOINT=http://127.0.0.1:38015/v1/metrics; %s", call)
			return c.WithExec([]string{"sh", "-c", script}, dagger.ContainerWithExecOpts{
				ExperimentalPrivilegedNesting: true,
			})
		}).
		Stdout(ctx)
	if err != nil {
		return
	}
	fmt.Println(stdout)
}
