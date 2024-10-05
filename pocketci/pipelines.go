package pocketci

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

func (s *Server) PipelineClaimHandler(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	slog.Info("checking list of pipelines")

	req := &PipelineClaimRequest{}
	if err := json.NewDecoder(r.Body).Decode(req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if len(s.pipelines) == 0 {
		slog.Info("no pipelines queued")
		w.WriteHeader(http.StatusNoContent)
		return
	}

	for i, pipeline := range s.pipelines {
		if pipeline.RunsOn != req.RunnerName {
			continue
		}

		slog.Info("pipeline match", slog.String("pipeline", pipeline.Name), slog.String("runner_name", req.RunnerName))

		s.pipelines = append(s.pipelines[:i], s.pipelines[i+1:]...)
		if err := json.NewEncoder(w).Encode(pipeline); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		return
	}

	slog.Info("no pipelines for runner", slog.String("runner_name", req.RunnerName))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) PipelineHandler(w http.ResponseWriter, r *http.Request) {
	req := &CreatePipelineRequest{}
	if err := json.NewDecoder(r.Body).Decode(req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.pipelines = append(s.pipelines, req)

	w.WriteHeader(http.StatusAccepted)

}
