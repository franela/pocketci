package main

import (
	"dagger/gha/internal/dagger"
	"encoding/json"

	"github.com/franela/pocketci/pocketci"
)

type Gha struct{}

type Pipeline struct {
	// +private
	Runner string
	// +private
	Changes []string
	// +private
	UseModule string
	// +private
	Name string
	// +private
	MatchActions []string
	// +private
	MatchOnPR bool
	// +private
	BaseBranches []string
	// +private
	MatchOnPush bool
	// +private
	MatchBranches []string
	// +private
	Exec string
	// +private
	PipelineDeps []string
}

type Action string

const (
	PROpened      Action = "opened"
	PRReopened    Action = "reopened"
	PRSynchronize Action = "synchronize"
)

// Returns a container that echoes whatever string argument is provided
func (m *Gha) Pipeline(name string) *Pipeline {
	return &Pipeline{Name: name}
}

func (m *Pipeline) RunsOn(name string) *Pipeline {
	m.Runner = name
	return m
}

func (m *Pipeline) OnPullRequest(actions ...Action) *Pipeline {
	a := []string{}
	for _, action := range actions {
		a = append(a, string(action))
	}
	m.MatchActions = a
	m.MatchOnPR = true
	return m
}

func (m *Pipeline) OnPullRequestAgainst(actions []Action, branches []string) *Pipeline {
	a := []string{}
	for _, action := range actions {
		a = append(a, string(action))
	}
	m.MatchActions = a
	m.MatchOnPR = true
	return m
}

func (m *Pipeline) OnChanges(paths ...string) *Pipeline {
	m.Changes = paths
	return m
}

func (m *Pipeline) OnPush(branches ...string) *Pipeline {
	m.MatchOnPush = true
	m.MatchBranches = branches
	return m
}

func (m *Pipeline) Module(path string) *Pipeline {
	m.UseModule = path
	return m
}

func (m *Pipeline) Call(exec string) *Pipeline {
	m.Exec = exec
	return m
}

func (m *Pipeline) After(pipelines ...*Pipeline) *Pipeline {
	deps := []string{}
	for _, p := range pipelines {
		deps = append(deps, p.Name)
	}
	m.PipelineDeps = deps
	return m
}

func (m *Gha) Pipelines(pipelines []*Pipeline) (*dagger.File, error) {
	ps := []pocketci.Pipeline{}

	for _, p := range pipelines {
		ps = append(ps, pocketci.Pipeline{
			Name:         p.Name,
			Runner:       p.Runner,
			Changes:      p.Changes,
			Module:       p.UseModule,
			Actions:      p.MatchActions,
			OnPR:         p.MatchOnPR,
			OnPush:       p.MatchOnPush,
			Branches:     p.MatchBranches,
			Exec:         []string{p.Exec},
			PipelineDeps: p.PipelineDeps,
		})
	}

	b, err := json.Marshal(ps)
	if err != nil {
		return nil, err
	}

	return dag.Container().WithNewFile("/pipelines.json", string(b)).File("/pipelines.json"), nil
}
