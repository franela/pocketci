// A generated module for Gha functions
//
// This module has been generated via dagger init and serves as a reference to
// basic module structure as you get started with Dagger.
//
// Two functions have been pre-created. You can modify, delete, or add to them,
// as needed. They demonstrate usage of arguments and return types using simple
// echo and grep commands. The functions can be called from the dagger CLI or
// from one of the SDKs.
//
// The first line in this comment block is a short description line and the
// rest is a long description with more detail on the module's purpose or usage,
// if appropriate. All modules should have a short description.

package main

import (
	"bytes"
	"context"
	"dagger/gha/internal/dagger"
	"encoding/json"
	"errors"
	"net/http"
	"slices"

	"github.com/bmatcuk/doublestar"
	"github.com/google/go-github/v61/github"
)

type Gha struct {
	GithubEvent *GithubEvent
}

type event struct {
	*Event
	Payload json.RawMessage `json:"payload"`
}

type Event struct {
	StringPayload  string   `json:"-"`
	EventType      string   `json:"event_type"`
	FilesChanged   []string `json:"files_changed"`
	RepositoryName string   `json:"repository_name"`
	Ref            string   `json:"ref"`
	SHA            string   `json:"sha"`
	BaseRef        string   `json:"base_ref,omitempty"`
	BaseSHA        string   `json:"base_sha,omitempty"`
	PrNumber       int      `json:"pr_number,omitempty"`
}

type GithubEvent struct {
	*Event
	PullRequest *PullRequest
}

type PullRequest struct {

	// Action is the action that was performed. Possible values are:
	// "assigned", "unassigned", "review_requested", "review_request_removed", "labeled", "unlabeled",
	// "opened", "edited", "closed", "ready_for_review", "locked", "unlocked", or "reopened".
	// If the action is "closed" and the "merged" key is "false", the pull request was closed with unmerged commits.
	// If the action is "closed" and the "merged" key is "true", the pull request was merged.
	// While webhooks are also triggered when a pull request is synchronized, Events API timelines
	// don't include pull request events with the "synchronize" action.
	Action      string
	Number      int
	PullRequest PullRequestSpec

	Repo  Repository
	Label string
}

type PullRequestSpec struct {
	Number         int
	State          string
	Locked         bool
	CreatedAt      string
	UpdatedAt      string
	ClosedAt       string
	MergedAt       string
	Labels         []string
	Merged         bool
	Mergeable      bool
	MergeableState string
	Head           *PullRequestBranch
	Base           *PullRequestBranch
}

type PullRequestBranch struct {
	Label string
	Ref   string
	SHA   string
	Repo  Repository
}

type PullRequestLabel struct {
	Name string
}

type Repository struct {
	Owner    User
	Name     string
	FullName string
}

type User struct {
	Login    string
	Name     string
	UserType string
}

func New(ctx context.Context, eventSrc *dagger.File) (*Gha, error) {
	contents, err := eventSrc.Contents(ctx)
	if err != nil {
		return nil, err
	}

	ev := &event{}
	err = json.Unmarshal([]byte(contents), ev)
	if err != nil {
		return nil, err
	}
	ge, err := github.ParseWebHook(ev.EventType, ev.Payload)
	if err != nil {
		return nil, err
	}

	ev.Event.StringPayload = string(ev.Payload)
	switch event := ge.(type) {
	case *github.PullRequestEvent:
		return &Gha{GithubEvent: &GithubEvent{
			Event:       ev.Event,
			PullRequest: fromGithubPullRequest(event),
		}}, nil
	default:
		return nil, errors.New("unsupported event type")

	}

}

type Pipeline struct {
	RunsOn        string
	Changes       []string
	Module        string
	Name          string
	Event         *GithubEvent
	Actions       []Action
	OnPullRequest bool
}

type pipelineRequest struct {
	RunsOn         string          `json:"runs_on"`
	Name           string          `json:"name"`
	Exec           string          `json:"exec"`
	RepositoryName string          `json:"repository_name"`
	Ref            string          `json:"ref"`
	SHA            string          `json:"sha"`
	BaseRef        string          `json:"base_ref"`
	BaseSHA        string          `json:"base_sha"`
	PrNumber       int             `json:"pr_number"`
	Module         string          `json:"module"`
	EventType      string          `json:"event_type"`
	Payload        json.RawMessage `json:"payload"`
}

type Action string

const (
	PROpened      Action = "opened"
	PRReopened    Action = "reopened"
	PRSynchronize Action = "synchronize"
)

// Returns a container that echoes whatever string argument is provided
func (m *Gha) WithPipeline(name string) *Pipeline {
	return &Pipeline{Name: name, Event: m.GithubEvent}
}

func (m *Pipeline) WithRunsOn(name string) *Pipeline {
	m.RunsOn = name
	return m
}

func (m *Pipeline) WithOnPullRequest(actions ...Action) *Pipeline {
	m.Actions = actions
	m.OnPullRequest = true
	return m
}

func (m *Pipeline) WithOnChanges(paths ...string) *Pipeline {
	m.Changes = paths
	return m
}

func (m *Pipeline) WithModule(path string) *Pipeline {
	m.Module = path
	return m
}

func (m *Pipeline) Call(ctx context.Context, exec string) error {
	switch {
	// a pull request was received and requested
	case m.Event.PullRequest != nil && m.OnPullRequest:
		pr := m.Event.PullRequest
		if len(m.Actions) != 0 && slices.Index(m.Actions, Action(pr.Action)) == -1 {
			return nil
		}
		if !Match(m.Event.FilesChanged, m.Changes...) {
			return nil
		}
		buf := bytes.NewBuffer([]byte{})
		if err := json.NewEncoder(buf).Encode(&pipelineRequest{
			RunsOn:         m.RunsOn,
			Name:           m.Name,
			Exec:           exec,
			RepositoryName: m.Event.RepositoryName,
			Ref:            m.Event.Ref,
			SHA:            m.Event.SHA,
			BaseRef:        m.Event.BaseRef,
			BaseSHA:        m.Event.BaseSHA,
			PrNumber:       m.Event.PrNumber,
			Module:         m.Module,
			EventType:      m.Event.EventType,
			Payload:        json.RawMessage([]byte(m.Event.StringPayload)),
		}); err != nil {
			return err
		}

		res, err := http.Post("http://172.17.0.1:8080/pipelines", "application/json", buf)
		if err != nil {
			return err
		}
		res.Body.Close()

		return nil
	default:
		return errors.New("nop")
	}
}

func fromGithubPullRequest(e *github.PullRequestEvent) *PullRequest {
	pr := &PullRequest{}
	pr.Action = *e.Action
	pr.Number = *e.Number

	var createdAt, updatedAt, closedAt, mergedAt string
	if e.PullRequest.CreatedAt != nil {
		createdAt = e.PullRequest.CreatedAt.String()
	}
	if e.PullRequest.UpdatedAt != nil {
		updatedAt = e.PullRequest.UpdatedAt.String()
	}
	if e.PullRequest.ClosedAt != nil {
		closedAt = e.PullRequest.ClosedAt.String()
	}
	if e.PullRequest.MergedAt != nil {
		mergedAt = e.PullRequest.MergedAt.String()
	}

	pr.PullRequest = PullRequestSpec{
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
		ClosedAt:  closedAt,
		MergedAt:  mergedAt,
		Labels:    []string{},
	}

	if e.PullRequest.Number != nil {
		pr.PullRequest.Number = *e.PullRequest.Number
	}
	if e.PullRequest.State != nil {
		pr.PullRequest.State = *e.PullRequest.State
	}
	if e.PullRequest.Locked != nil {
		pr.PullRequest.Locked = *e.PullRequest.Locked
	}
	if e.PullRequest.Merged != nil {
		pr.PullRequest.Merged = *e.PullRequest.Merged
	}
	if e.PullRequest.Mergeable != nil {
		pr.PullRequest.Mergeable = *e.PullRequest.Mergeable
	}
	if e.PullRequest.MergeableState != nil {
		pr.PullRequest.MergeableState = *e.PullRequest.MergeableState
	}

	if len(e.PullRequest.Labels) > 0 {
		for _, label := range e.PullRequest.Labels {
			pr.PullRequest.Labels = append(pr.PullRequest.Labels, *label.Name)
		}
	}

	if e.PullRequest.Base != nil {
		repo := Repository{
			Owner: User{
				Login:    *e.PullRequest.Base.Repo.Owner.Login,
				UserType: *e.PullRequest.Base.Repo.Owner.Type,
			},
		}

		if e.PullRequest.Base.Repo.Owner.Name != nil {
			repo.Owner.Name = *e.PullRequest.Base.Repo.Owner.Name
		}

		pr.PullRequest.Base = &PullRequestBranch{
			Label: *e.PullRequest.Base.Label,
			Ref:   *e.PullRequest.Base.Ref,
			SHA:   *e.PullRequest.Base.SHA,
			Repo:  repo,
		}
	}

	if e.PullRequest.Head != nil {
		repo := Repository{
			Owner: User{
				Login:    *e.PullRequest.Head.Repo.Owner.Login,
				UserType: *e.PullRequest.Head.Repo.Owner.Type,
			},
		}

		if e.PullRequest.Head.Repo.Owner.Name != nil {
			repo.Owner.Name = *e.PullRequest.Head.Repo.Owner.Name
		}
		pr.PullRequest.Head = &PullRequestBranch{
			Label: *e.PullRequest.Head.Label,
			Ref:   *e.PullRequest.Head.Ref,
			SHA:   *e.PullRequest.Head.SHA,
			Repo:  repo,
		}
	}

	pr.Repo = Repository{
		Owner: User{
			Login: *e.Repo.Owner.Login,
			Name:  *e.Repo.Name,
		},
	}

	if e.Label != nil {
		pr.Label = *e.Label.Name
	}

	return pr
}

func Match(files []string, patterns ...string) bool {
	for _, file := range files {
		for _, pattern := range patterns {
			match, err := doublestar.PathMatch(pattern, file)
			if err != nil {
				continue
			}
			if match {
				return true
			}
		}
	}
	return false
}
