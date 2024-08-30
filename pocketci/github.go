package pocketci

import (
	"encoding/json"

	"dagger.io/dagger"
)

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
