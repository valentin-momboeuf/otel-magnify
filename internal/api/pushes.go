package api

import (
	"net/http"
)

// handleListPushActivity returns per-day push counts for the dashboard chart.
// Only window=7d is supported today; the query param exists to future-proof
// the endpoint without breaking the client contract.
func (a *API) handleListPushActivity(w http.ResponseWriter, r *http.Request) {
	window := r.URL.Query().Get("window")
	if window == "" {
		window = "7d"
	}
	if window != "7d" {
		respondError(w, http.StatusBadRequest, "unsupported window; only 7d is supported")
		return
	}

	points, err := a.db.GetPushActivity(7)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to compute push activity")
		return
	}
	respondJSON(w, http.StatusOK, points)
}
