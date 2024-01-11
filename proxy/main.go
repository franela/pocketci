package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
)

var (
	repoName = flag.String("repo-name", "", "repository name")
	server   = flag.String("server", "", "server to proxy to")
	port     = flag.Int("port", 8080, "port to listen on")
)

func main() {
	flag.Parse()

	if *server == "" {
		log.Fatal("server is required")
	}

	u, err := url.Parse(*server)
	if err != nil {
		log.Fatalf("server url is invalid: %s", err)
	}

	http.HandleFunc("/", gitCloneProxy("/"+*repoName, *server, httputil.NewSingleHostReverseProxy(u)))
	http.ListenAndServe(fmt.Sprintf(":%d", *port), nil)
}

type GithubWebhook struct {
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
}

// gitCloneProxy returns a handler that will first clone a git repository into
// the specified directory and then proxy the request to the reverse proxy.
func gitCloneProxy(dir string, serverURL string, rproxy http.Handler) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		gh := &GithubWebhook{}
		if err := json.NewDecoder(r.Body).Decode(gh); err != nil {
			log.Printf("failed to decode JSON payload: %s", err.Error())
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		rmCmd := exec.Command("rm", "-rf", dir)
		rmCmd.Stdout = os.Stderr
		rmCmd.Stderr = os.Stderr
		if err := rmCmd.Run(); err != nil {
			log.Printf("failed to clean local git repo: %s. Moving on to cloning", err.Error())
		}

		cmd := exec.Command("git", "clone", "https://github.com/"+gh.Repository.FullName, dir)
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			log.Printf("failed to clone git repo: %s", err.Error())
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		res, err := http.Get(serverURL + "/reload")
		if err != nil {
			log.Printf("failed to reload webhook server: %s", err.Error())
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		res.Body.Close()

		rproxy.ServeHTTP(w, r)
	}
}
