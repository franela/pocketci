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
}

func New(ctx context.Context, payload *dagger.File) (*Pocketci, error) {
	e, err := parsePayload(ctx, payload)
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

	pullEvent, ok := ghEvent.(*github.PullRequestEvent)
	if !ok {
		return nil, fmt.Errorf("got an event of type %T instead of a PullRequestEvent", ghEvent)
	}

	pr := fromGithubPullRequest(pullEvent)
	pr.Event = Event{
		RepoName:  e.RepoName,
		Changes:   e.Changes,
		EventType: e.EventType,
	}
	return &Pocketci{PullRequestEvent: pr}, nil
}

func (m *Pocketci) OnPullRequest() *PullRequestEvent {
	return m.PullRequestEvent
}

func parsePayload(ctx context.Context, payload *dagger.File) (*event, error) {
	contents, err := payload.Contents(ctx)
	if err != nil {
		return nil, err
	}

	e := &event{}
	if err := json.Unmarshal([]byte(contents), &e); err != nil {
		return nil, err
	}

	return e, nil
}
