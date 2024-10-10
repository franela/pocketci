package pocketci

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/storage/memory"
)

type watcher struct {
	repoURL    string
	eventChan  chan ciEvent
	lastState  map[string]plumbing.Hash
	repository *git.Repository
}

type ciEvent struct {
	Type   string
	Ref    string
	Hash   plumbing.Hash
	Parent plumbing.Hash
}

func newWatcher(repoURL string) (*watcher, error) {
	repo, err := git.Clone(memory.NewStorage(), nil, &git.CloneOptions{
		URL:        repoURL,
		RemoteName: "origin",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to clone repository: %w", err)
	}

	return &watcher{
		repoURL:    repoURL,
		eventChan:  make(chan ciEvent, 100),
		lastState:  make(map[string]plumbing.Hash),
		repository: repo,
	}, nil
}

func (w *watcher) fetch() {
	remote, err := w.repository.Remote("origin")
	if err != nil {
		return
	}

	refs, err := remote.ListContext(context.Background(), &git.ListOptions{})
	if err != nil {
		return
	}

	for _, ref := range refs {
		if ref.Type() != plumbing.HashReference {
			continue
		}

		newHash := ref.Hash()
		refName := ref.Name().String()

		// only process the ref if it changed, if it did not exist we just need
		// to process the last one, which is the current one we fetched
		// since we are initting we shouldn't actually trigger a commit the first
		// time the repository is installed
		if oldHash, exists := w.lastState[refName]; exists && oldHash != newHash {
			slog.Info("ref changed", slog.String("ref", refName),
				slog.String("old_hash", oldHash.String()), slog.String("new_hash", newHash.String()))

			w.handleRef(refName, oldHash, newHash)
		}

		w.lastState[refName] = newHash
	}

	if err != nil {
		fmt.Printf("Error processing references: %v\n", err)
	}
}

func (w *watcher) handleRef(refName string, oldHash, newHash plumbing.Hash) {
	if refName[:11] == "refs/heads/" {
		w.lsCommits(refName, oldHash, newHash)
	} else if refName[:10] == "refs/tags/" {
		w.sendEvent("tag", refName[10:], newHash, plumbing.ZeroHash)
	}
}

func (w *watcher) lsCommits(refName string, oldHash, newHash plumbing.Hash) {
	if err := w.repository.Fetch(&git.FetchOptions{
		RemoteName: "origin",
		RefSpecs:   []config.RefSpec{config.RefSpec("+" + refName + ":" + refName)},
	}); err != nil {
		return
	}

	commits, err := w.repository.Log(&git.LogOptions{From: newHash})
	if err != nil {
		fmt.Printf("Error getting commit history: %v\n", err)
		return
	}

	var newCommits []plumbing.Hash
	err = commits.ForEach(func(c *object.Commit) error {
		if c.Hash == oldHash {
			return storer.ErrStop
		}
		newCommits = append(newCommits, c.Hash)
		return nil
	})

	if err != nil && err != storer.ErrStop {
		fmt.Printf("Error processing commits: %v\n", err)
		return
	}

	for i := len(newCommits) - 1; i >= 0; i-- {
		parentHash := plumbing.ZeroHash
		if i < len(newCommits)-1 {
			parentHash = newCommits[i+1]
		} else if i == len(newCommits)-1 && oldHash != plumbing.ZeroHash {
			parentHash = oldHash
		}
		w.sendEvent("commit", refName[11:], newCommits[i], parentHash)
	}
}

func (w *watcher) sendEvent(eventType, ref string, hash, parent plumbing.Hash) {
	w.eventChan <- ciEvent{
		Type:   eventType,
		Ref:    ref,
		Hash:   hash,
		Parent: parent,
	}
}

func (w *watcher) Events() <-chan ciEvent {
	return w.eventChan
}
