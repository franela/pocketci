package pocketci

import (
	"encoding/json"

	"dagger.io/dagger"
)

type Event struct {
	RepositoryName string `json:"repository_name"`
	Ref            string `json:"ref"`
	SHA            string `json:"sha"`
	BaseRef        string `json:"base_ref,omitempty"`
	BaseSHA        string `json:"base_sha,omitempty"`
	PrNumber       int    `json:"pr_number,omitempty"`

	Repository *dagger.Directory `json:"-"`
	Changes    []string          `json:"files_changed"`
	Payload    json.RawMessage   `json:"payload"`

	EventType string `json:"event_type"`

	EnvVariables map[string]string `json:"-"`
}

// CreatePipelineRequest is the payload received on pipeline creation.
type CreatePipelineRequest struct {
	RunsOn         string `json:"runs_on"`
	Name           string `json:"name"`
	Exec           string `json:"exec"`
	RepositoryName string `json:"repository_name"`
	Ref            string `json:"ref"`
	SHA            string `json:"sha"`
	BaseRef        string `json:"base_ref"`
	BaseSHA        string `json:"base_sha"`
	PrNumber       int    `json:"pr_number"`
	Module         string `json:"module"`
	Workdir        string `json:"workdir"`
	EventType      string `json:"event_type"`
}

// PipelineClaimRequest is the payload received when a runner wants to claim
// a pipeline.
type PipelineClaimRequest struct {
	RunnerName string `json:"runner_name"`
}

const (
	GithubPullRequest = "pull_request"
	GithubPush        = "push"
	GithubRelease     = "release"
)

type GithubEvent struct {
	Payload json.RawMessage `json:"payload"`

	EventType string   `json:"event_type"`
	Filter    string   `json:"filter"`
	Changes   []string `json:"changes"`

	Repository     *dagger.Directory `json:"-"`
	RepositoryName string            `json:"repository_name"`
	Ref            string            `json:"ref"`
	SHA            string            `json:"sha"`
	BaseRef        string            `json:"base_ref,omitempty"`
	BaseSHA        string            `json:"base_sha,omitempty"`
	PrNumber       int               `json:"pr_number,omitempty"`

	Variables map[string]string `json:"-"`
}
