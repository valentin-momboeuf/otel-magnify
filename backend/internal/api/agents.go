package api

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"otel-magnify/internal/auth"
	"otel-magnify/internal/validator"
	"otel-magnify/pkg/models"
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

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		respondError(w, 400, "failed to read body")
		return
	}
	defer r.Body.Close()

	if len(body) == 0 {
		respondError(w, 400, "empty config body")
		return
	}

	if a.opamp == nil {
		respondError(w, 503, "OpAMP server not available")
		return
	}

	// Safety net: refuse to push a config that fails light validation.
	// The frontend should have called /validate first for UX feedback;
	// this blocks API-level bypass.
	var available *models.AvailableComponents
	if agent, err := a.db.GetAgent(agentID); err == nil {
		available = agent.AvailableComponents
	}
	if result := validator.Validate(body, available); !result.Valid {
		respondJSON(w, 400, map[string]any{
			"error":             "configuration failed validation",
			"validation_errors": result.Errors,
		})
		return
	}

	sum := sha256.Sum256(body)
	hash := hex.EncodeToString(sum[:])

	pushedBy := ""
	if claims := auth.ClaimsFromContext(r.Context()); claims != nil {
		pushedBy = claims.Email
	}

	// Persist the config (dedup by hash). Ignore errors on duplicate hash —
	// if the config row is genuinely missing, the RecordAgentConfig FK would fail below.
	_ = a.db.CreateConfig(models.Config{
		ID:        hash,
		Name:      fmt.Sprintf("push-%s", hash[:8]),
		Content:   string(body),
		CreatedAt: time.Now().UTC(),
		CreatedBy: pushedBy,
	})

	if err := a.db.RecordAgentConfig(models.AgentConfig{
		AgentID:  agentID,
		ConfigID: hash,
		Status:   "pending",
		PushedBy: pushedBy,
	}); err != nil {
		respondError(w, 500, "failed to record push")
		return
	}

	if err := a.opamp.PushConfig(r.Context(), agentID, body); err != nil {
		_ = a.db.UpdateAgentConfigStatus(agentID, hash, "failed", err.Error())
		respondError(w, 502, err.Error())
		return
	}

	respondJSON(w, 202, map[string]string{
		"status":      "config push initiated",
		"config_hash": hash,
	})
}

// handleValidateConfig runs the light validator against a candidate YAML for
// an agent, using the agent's reported AvailableComponents when present.
// Always returns 200 with a Result body — the client inspects result.valid.
func (a *API) handleValidateConfig(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "id")

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		respondError(w, 400, "failed to read body")
		return
	}
	defer r.Body.Close()
	if len(body) == 0 {
		respondError(w, 400, "empty config body")
		return
	}

	var available *models.AvailableComponents
	if agent, err := a.db.GetAgent(agentID); err == nil {
		available = agent.AvailableComponents
	} else if !errors.Is(err, sql.ErrNoRows) {
		respondError(w, 500, "failed to load agent")
		return
	}

	respondJSON(w, 200, validator.Validate(body, available))
}

func (a *API) handleGetAgentConfigHistory(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	history, err := a.db.GetAgentConfigHistory(id)
	if err != nil {
		respondError(w, 500, "failed to get config history")
		return
	}
	respondJSON(w, 200, history)
}
