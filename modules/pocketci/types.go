package main

import (
	"encoding/json"
	"fmt"

	"github.com/google/go-github/v61/github"
)

type Event struct {
	EventType string   `json:"event_type"`
	Changes   []string `json:"changes"`
	RepoName  string   `json:"repo_name"`
}

// TODO: For some very **VERY** bizarre reason to me, embedding `Event` into this struct
// breaks the unmarshaling of this object leaving the `payload` field empty. Adding
// these fields one by one works without any issues
// It is very strange that the ONLY field not set is the `event`, but if you parse
// it as a map or something else it works perfectly. The problem is embedding
// the event struct. I tried to repro on the go playground and couldn't: https://play.golang.com/p/7jEKgPxhdYE
type event struct {
	EventType string          `json:"event_type"`
	Changes   []string        `json:"changes"`
	RepoName  string          `json:"repo_name"`
	Payload   json.RawMessage `json:"payload"`
}

func fromGithubPullRequest(e *github.PullRequestEvent) *PullRequestEvent {
	fmt.Printf("%+v\n", *e)
	pr := &PullRequestEvent{}
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

	pr.PullRequest = PullRequest{
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

type PullRequestEvent struct {
	Event

	// Action is the action that was performed. Possible values are:
	// "assigned", "unassigned", "review_requested", "review_request_removed", "labeled", "unlabeled",
	// "opened", "edited", "closed", "ready_for_review", "locked", "unlocked", or "reopened".
	// If the action is "closed" and the "merged" key is "false", the pull request was closed with unmerged commits.
	// If the action is "closed" and the "merged" key is "true", the pull request was merged.
	// While webhooks are also triggered when a pull request is synchronized, Events API timelines
	// don't include pull request events with the "synchronize" action.
	Action      string
	Number      int
	PullRequest PullRequest

	Repo  Repository
	Label string
}

type PullRequest struct {
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