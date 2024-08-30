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

type Event struct {
	RepositoryName string            `json:"repository_name"`
	Repository     *dagger.Directory `json:"-"`
	Changes        []string          `json:"-"`
	Payload        json.RawMessage   `json:"payload"`

	Vendor    string `json:"vendor"`
	EventType string `json:"event_type"`
	Filter    string `json:"filter"`

	EnvVariables map[string]string `json:"-"`
}

// Dispatcher receives a list of functions and calls each one. Whether the function
// calls happen sync or async is up to the implementer.
// NOTE: if we eventually want to support remote function calls through a queue
// sort of system this interface (and the whole clone approach) will likely have
// to change. We can't really package the dagger.Directory of the repository through
// a queue so that would mean the dispatcher would have to re-clone the repo.
// Not a huge deal, but probably worth to spend some time and think how this could
// be re-architected.
type Dispatcher interface {
	Dispatch(ctx context.Context, spec *Spec, functions []Function, event *Event) error
}

type LocalDispatcher struct {
	dag         *dagger.Client
	parallelism int
}

func (ld *LocalDispatcher) Dispatch(ctx context.Context, spec *Spec, functions []Function, event *Event) error {
	payload, err := json.Marshal(event.Payload)
	if err != nil {
		return fmt.Errorf("could not marshal raw event payload: %s", err)
	}

	var g errgroup.Group
	g.SetLimit(ld.parallelism)
	for _, fn := range functions {
		g.Go(func(fn Function) func() error {
			return func() error {
				slog.Info("launching pocketci agent container dispatch call",
					slog.String("repository", event.RepositoryName), slog.String("function", fn.Name),
					slog.String("event_type", event.EventType), slog.String("filter", event.Filter))

				call := fmt.Sprintf("dagger call -m %s --progress plain %s %s --src . --event-trigger /payload.json", spec.ModulePath, fn.Name, fn.Args)
				stdout, err := AgentContainer(ld.dag).
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
