package api

import (
	"database/sql"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
)

func (a *API) handleListAgents(w http.ResponseWriter, r *http.Request) {
	agents, err := a.db.ListAgents()
	if err != nil {
		respondError(w, 500, "failed to list agents")
		return
	}
	respondJSON(w, 200, agents)
}

func (a *API) handleGetAgent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	agent, err := a.db.GetAgent(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			respondError(w, 404, "agent not found")
			return
		}
		respondError(w, 500, "failed to get agent")
		return
	}
	respondJSON(w, 200, agent)
}

func (a *API) handlePushConfig(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "id")

	body, err := io.ReadAll(r.Body)
	if err != nil {
		respondError(w, 400, "failed to read body")
		return
	}
	defer r.Body.Close()

	if a.opamp == nil {
		respondError(w, 503, "OpAMP server not available")
		return
	}

	if err := a.opamp.PushConfig(r.Context(), agentID, body); err != nil {
		respondError(w, 502, err.Error())
		return
	}

	respondJSON(w, 202, map[string]string{"status": "config push initiated"})
}
