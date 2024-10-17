package main

import (
	"dagger/gha/internal/dagger"
	"encoding/json"
)

type Gha struct{}

type Pipeline struct {
	Runner       string   `json:"runner"`
	Changes      []string `json:"changes"`
	Module       string   `json:"module"`
	Name         string   `json:"name"`
	Actions      []Action `json:"pr_actions"`
	OnPR         bool     `json:"on_pr"`
	OnPush       bool     `json:"on_push"`
	Branches     []string `json:"branches"`
	Exec         string   `json:"exec"`
	PipelineDeps []string `json:"after"`
}

type Action string

const (
	PROpened      Action = "opened"
	PRReopened    Action = "reopened"
	PRSynchronize Action = "synchronize"
)

// Returns a container that echoes whatever string argument is provided
func (m *Gha) WithPipeline(name string) *Pipeline {
	return &Pipeline{Name: name}
}

func (m *Pipeline) WithRunsOn(name string) *Pipeline {
	m.Runner = name
	return m
}

func (m *Pipeline) WithOnPullRequest(actions ...Action) *Pipeline {
	m.Actions = actions
	m.OnPR = true
	return m
}

func (m *Pipeline) WithOnChanges(paths ...string) *Pipeline {
	m.Changes = paths
	return m
}

func (m *Pipeline) WithOnPush(branches ...string) *Pipeline {
	m.OnPush = true
	m.Branches = branches
	return m
}

func (m *Pipeline) WithModule(path string) *Pipeline {
	m.Module = path
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
	b, err := json.Marshal(pipelines)
	if err != nil {
		return nil, err
	}

	return dag.Container().WithNewFile("/pipelines.json", string(b)).File("/pipelines.json"), nil
}
