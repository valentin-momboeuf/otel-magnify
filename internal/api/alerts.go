// Package api wires the chi-based REST router, WebSocket hub, and HTTP handlers exposed to the SPA.
package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func (a *API) handleListAlerts(w http.ResponseWriter, r *http.Request) {
	includeResolved := r.URL.Query().Get("include_resolved") == "true"
	alerts, err := a.db.ListAlerts(includeResolved)
	if err != nil {
		respondError(w, 500, "failed to list alerts")
		return
	}
	respondJSON(w, 200, alerts)
}

func (a *API) handleResolveAlert(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := a.db.ResolveAlert(id); err != nil {
		respondError(w, 500, "failed to resolve alert")
		return
	}
	respondJSON(w, 200, map[string]string{"status": "resolved"})
}
