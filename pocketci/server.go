package pocketci

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"dagger.io/dagger"
)

const GithubEventTypeHeader = "X-Github-Event"

type Views struct {
	List []View `json:"list"`
}

type View struct {
	Name   string `json:"name"`
	Active bool   `json:"active"`
}

type Event struct {
	EventType string   `json:"event_type"`
	Changes   []string `json:"changes"`
	Views     Views    `json:"views"`
}

type event struct {
	Event
	Payload json.RawMessage `json:"payload"`
}

type Server struct {
	agent *Agent
	opts  ServerOptions
}

type ServerOptions struct {
	GithubUsername string
	GithubPassword string
}

func NewServer(dag *dagger.Client, opts ServerOptions) (*Server, error) {
	// warmup the container that will be used for each request
	if _, err := AgentContainer(dag).Sync(context.Background()); err != nil {
		return nil, fmt.Errorf("warmup failed: %w", err)
	}

	return &Server{
		agent: NewAgent(dag),
		opts:  opts,
	}, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	sig := r.Header.Get("X-Hub-Signature")
	if sig == "" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	if err := validateGithubSignature(sig, os.Getenv("X_HUB_SIGNATURE")); err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	b, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("failed to get request body: %s", err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	r.Body = io.NopCloser(bytes.NewBuffer(b))

	go func() {
		githubEvent := &event{
			Event: Event{
				EventType: r.Header.Get(GithubEventTypeHeader),
			},
			Payload: json.RawMessage(b),
		}
		ctx := context.Background()
		if err := s.agent.HandleGithub(ctx, s.agent.CreateGithubSecret(s.opts.GithubUsername, s.opts.GithubPassword), githubEvent); err != nil {
			log.Printf("failed to handle github request: %s\n", err)
		}
	}()

	w.WriteHeader(http.StatusAccepted)
}

func validateGithubSignature(signature string, secret string) error {
	signature = strings.TrimPrefix(signature, "sha1=")

	mac := hmac.New(sha1.New, []byte(secret))

	_, err := mac.Write([]byte(signature))
	if err != nil {
		return err
	}

	actualMAC := hex.EncodeToString(mac.Sum(nil))

	if hmac.Equal([]byte(signature), []byte(actualMAC)) {
		return err
	}

	return nil
}
