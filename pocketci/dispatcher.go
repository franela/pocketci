package pocketci

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"dagger.io/dagger"
	"github.com/iancoleman/strcase"
	"golang.org/x/sync/errgroup"
)

// Dispatcher receives a list of functions and is in charge of making sure
// each function call happens at most once. Whether they happen sync or async
// is up to the implementation.
// NOTE: if we eventually want to support remote function calls through a queue
// sort of system this interface (and the whole clone approach) will likely have
// to change. We can't really package the dagger.Directory of the repository through
// a queue so that would mean the dispatcher would have to re-clone the repo.
// Not a huge deal, but probably worth to spend some time and think how this could
// be re-architected.
type Dispatcher interface {
	Dispatch(ctx context.Context, spec *Spec, functions []Function, event *Event) error
}

// LocalDispatcher makes each of the function calls directly on the host.
type LocalDispatcher struct {
	dag         *dagger.Client
	parallelism int
}

func (ld *LocalDispatcher) Dispatch(ctx context.Context, spec *Spec, functions []Function, event *Event) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("could not marshal raw event payload: %s", err)
	}

	api := ld.dag.Host().Service([]dagger.PortForward{
		{Backend: 8080, Frontend: 8080, Protocol: dagger.Tcp},
	}, dagger.HostServiceOpts{Host: "localhost"})

	var g errgroup.Group
	g.SetLimit(ld.parallelism)
	for _, fn := range functions {
		g.Go(func(fn Function) func() error {
			return func() error {
				slog.Info("launching pocketci agent container dispatch call",
					slog.String("repository", event.RepositoryName), slog.String("function", fn.Name),
					slog.String("event_type", event.EventType))

				call := fmt.Sprintf("dagger call -vvv -m %s --progress plain %s %s --event-trigger /payload.json", spec.ModulePath, fn.Name, fn.Args)
				stdout, err := AgentContainer(ld.dag).
					WithServiceBinding("pocketci", api).
					WithEnvVariable("_POCKETCI_CP_URL", "http://pocketci:8080/pipelines").
					WithEnvVariable("CACHE_BUST", time.Now().String()).
					WithEnvVariable("DAGGER_CLOUD_TOKEN", os.Getenv("DAGGER_CLOUD_TOKEN")).
					WithDirectory("/"+event.RepositoryName, event.Repository).
					WithWorkdir("/"+event.RepositoryName).
					WithNewFile("/raw-payload.json", string(event.Payload)).
					WithNewFile("/payload.json", string(payload)).
					WithEnvVariable("CI", "pocketci").
					With(func(c *dagger.Container) *dagger.Container {
						for key, val := range event.EnvVariables {
							c = c.WithEnvVariable(key, val)
						}
						script := fmt.Sprintf("unset TRACEPARENT;unset OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf;unset OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:38015;unset OTEL_EXPORTER_OTLP_TRACES_PROTOCOL=http/protobuf;unset OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=http://127.0.0.1:38015/v1/traces;unset OTEL_EXPORTER_OTLP_TRACES_LIVE=1;unset OTEL_EXPORTER_OTLP_LOGS_PROTOCOL=http/protobuf;unset OTEL_EXPORTER_OTLP_LOGS_ENDPOINT=http://127.0.0.1:38015/v1/logs;unset OTEL_EXPORTER_OTLP_METRICS_PROTOCOL=http/protobuf;unset OTEL_EXPORTER_OTLP_METRICS_ENDPOINT=http://127.0.0.1:38015/v1/metrics; %s", call)
						for _, secret := range spec.Secrets {
							c = c.WithSecretVariable(secret.FromEnv, ld.dag.SetSecret(secret.Name, os.Getenv(secret.FromEnv)))
							script = fmt.Sprintf("%s --%s env:%s", script, strcase.ToKebab(secret.Name), secret.FromEnv)
						}
						return c.WithExec([]string{"sh", "-c", script}, dagger.ContainerWithExecOpts{
							ExperimentalPrivilegedNesting: true,
						})
					}).
					Stdout(ctx)
				if err != nil {
					return err
				}

				fmt.Printf("$ %s\n%s", call, stdout)
				return nil
			}

		}(fn))
	}

	return g.Wait()
}
