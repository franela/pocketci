package pocketci

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
)

func (s *Server) PipelineClaimHandler(w http.ResponseWriter, r *http.Request) {
	req := &PipelineClaimRequest{}
	if err := json.NewDecoder(r.Body).Decode(req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	pipeline := s.orchestrator.Dispatcher.GetPipeline(r.Context(), req.RunnerName)
	if pipeline == nil {
		slog.Info("no pipelines for runner", slog.String("runner_name", req.RunnerName))
		w.WriteHeader(http.StatusNoContent)
		return
	}

	slog.Info("pipeline match", slog.String("pipeline", pipeline.Name), slog.String("runner_name", req.RunnerName))

	if err := json.NewEncoder(w).Encode(pipeline); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (s *Server) PipelineDoneHandler(w http.ResponseWriter, r *http.Request) {
	pipelineID, err := strconv.Atoi(r.PathValue("pipeline_id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := s.orchestrator.Dispatcher.PipelineDone(r.Context(), pipelineID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
