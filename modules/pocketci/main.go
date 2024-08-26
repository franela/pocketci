package main

import (
	"context"
	"dagger/pocketci/internal/dagger"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/go-github/v61/github"
)

type Pocketci struct {
	PullRequestEvent *PullRequestEvent
	CommitPush       *CommitPush
}

func New(ctx context.Context, eventTrigger *dagger.File) (*Pocketci, error) {
	e, err := parseEventTrigger(ctx, eventTrigger)
	if err != nil {
		return nil, err
	}

	if len(e.Payload) == 0 {
		return nil, errors.New("empty payload")
	}

	ghEvent, err := github.ParseWebHook(e.EventType, e.Payload)
	if err != nil {
		return nil, err
	}

	switch event := ghEvent.(type) {
	case *github.PullRequestEvent:
		pr := fromGithubPullRequest(event)
		pr.Event = Event{
			RepoName:  e.RepoName,
			Changes:   e.Changes,
			EventType: e.EventType,
		}
		return &Pocketci{PullRequestEvent: pr}, nil
	case *github.PushEvent:
		commitPush := fromGithubPushEvent(event)
		return &Pocketci{CommitPush: commitPush}, nil
	default:
		return nil, fmt.Errorf("event of type %T is not yet supported", event)
	}
}

func (m *Pocketci) OnPullRequest() *PullRequestEvent {
	return m.PullRequestEvent
}

func (m *Pocketci) OnCommitPush() *CommitPush {
	return m.CommitPush
}

func parseEventTrigger(ctx context.Context, eventTrigger *dagger.File) (*event, error) {
	contents, err := eventTrigger.Contents(ctx)
	if err != nil {
		return nil, err
	}

	e := &event{}
	if err := json.Unmarshal([]byte(contents), &e); err != nil {
		return nil, err
	}

	return e, nil
}
