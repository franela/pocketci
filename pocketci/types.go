package pocketci

import (
	"dagger.io/dagger"
	"github.com/google/go-github/v61/github"
)

// CreatePipelineRequest is the payload received on pipeline creation.
type PipelineDoneRequest struct {
	ID int `json:"id"`
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
	RawPayload []byte

	EventType string   `json:"event_type"`
	Changes   []string `json:"changes"`

	Repository     *dagger.Directory `json:"-"`
	RepositoryName string            `json:"repository_name"`

	PullRequestEvent *github.PullRequestEvent
	PushEvent        *github.PushEvent

	Variables map[string]string

	Branch string
	SHA    string
}
