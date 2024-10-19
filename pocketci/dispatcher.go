package pocketci

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
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
	Dispatch(ctx context.Context, rawEvent json.RawMessage, gitInfo GitInfo, pipelines []*Pipeline) error
	GetPipeline(ctx context.Context, runner string) *PocketciPipeline
	PipelineDone(ctx context.Context, id int) error
}

// LocalDispatcher makes each of the function calls directly on the host.
type LocalDispatcher struct {
	queuedMu sync.RWMutex
	queued   []*PocketciPipeline

	runningMu sync.RWMutex
	running   map[int]*PocketciPipeline

	doneMu sync.RWMutex
	done   map[int]*PocketciPipeline

	lastID atomic.Int64
}

func NewLocalDispatcher() *LocalDispatcher {
	return &LocalDispatcher{
		queued:  []*PocketciPipeline{},
		running: map[int]*PocketciPipeline{},
		done:    map[int]*PocketciPipeline{},
	}
}

type PocketciPipeline struct {
	ID         int             `json:"id"`
	Name       string          `json:"name"`
	Call       string          `json:"call"`
	Parents    []int           `json:"parents"`
	Repository string          `json:"repository"`
	Runner     string          `json:"runner"`
	Changes    []string        `json:"changes"`
	Module     string          `json:"module"`
	RawEvent   json.RawMessage `json:"raw_event"`

	pipelineDeps []string

	GitInfo GitInfo `json:"git_info"`
}

func (ld *LocalDispatcher) GetPipeline(ctx context.Context, runner string) *PocketciPipeline {
	return ld.getPipeline(ctx, runner, 0)
}

func (ld *LocalDispatcher) getPipeline(ctx context.Context, runner string, id int) *PocketciPipeline {
	ld.queuedMu.Lock()
	if len(ld.queued) <= id {
		ld.queuedMu.Unlock()
		slog.Debug("no pipeline found", slog.String("runner", runner))
		return nil
	}

	pipeline := ld.queued[id]
	if pipeline.Runner != "" && pipeline.Runner != runner {
		ld.queuedMu.Unlock()
		slog.Info(fmt.Sprintf("skipping pipeline. Requested runner %s but had %s", runner, pipeline.Runner))
		return ld.getPipeline(ctx, runner, id+1)
	}

	ld.doneMu.RLock()
	allDone := true
	for _, parent := range pipeline.Parents {
		_, ok := ld.done[parent]
		allDone = allDone && ok
	}
	ld.doneMu.RUnlock()

	if !allDone {
		ld.queuedMu.Unlock()
		return ld.getPipeline(ctx, runner, id+1)
	}

	switch id {
	case 0:
		ld.queued = ld.queued[1:]
	case len(ld.queued):
		ld.queued = ld.queued[:id]
	default:
		ld.queued = ld.queued[0:id]
		ld.queued = ld.queued[id:]
	}
	ld.queuedMu.Unlock()

	ld.runningMu.Lock()
	ld.running[pipeline.ID] = pipeline
	ld.runningMu.Unlock()
	return pipeline
}

func (ld *LocalDispatcher) PipelineDone(ctx context.Context, id int) error {
	ld.runningMu.Lock()
	pipeline, ok := ld.running[id]
	if !ok {
		ld.runningMu.Unlock()
		return errors.New("pipeline not found")
	}

	delete(ld.running, id)
	ld.runningMu.Unlock()

	ld.doneMu.Lock()
	ld.done[id] = pipeline
	ld.doneMu.Unlock()

	return nil
}

func (ld *LocalDispatcher) Dispatch(ctx context.Context, rawEvent json.RawMessage, gitInfo GitInfo, pipelines []*Pipeline) error {
	cache := map[string][]*PocketciPipeline{}
	newPipelines := []*PocketciPipeline{}
	for _, p := range pipelines {
		for _, cmd := range p.Exec {
			cmd = strings.TrimSpace(cmd)

			pci := &PocketciPipeline{
				RawEvent:     rawEvent,
				ID:           int(ld.lastID.Add(1)),
				Call:         cmd,
				Name:         p.Name,
				Repository:   p.Repository,
				Runner:       p.Runner,
				Changes:      p.Changes,
				Module:       p.Module,
				pipelineDeps: p.PipelineDeps,
				GitInfo:      gitInfo,
			}

			if len(cache[p.Name]) == 0 {
				cache[p.Name] = []*PocketciPipeline{}
			}
			cache[p.Name] = append(cache[p.Name], pci)

			slog.Info("new pipeline", slog.String("name", pci.Name), slog.String("call", pci.Call))
			newPipelines = append(newPipelines, pci)
		}
	}

	for _, p := range newPipelines {
		if len(p.pipelineDeps) == 0 {
			continue
		}

		for _, parent := range p.pipelineDeps {
			for _, parentPipeline := range cache[parent] {
				p.Parents = append(p.Parents, parentPipeline.ID)
			}
		}
	}

	ld.queuedMu.Lock()
	defer ld.queuedMu.Unlock()
	ld.queued = append(ld.queued, newPipelines...)

	return nil
}
