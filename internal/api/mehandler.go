package api

import (
	"net/http"

	"github.com/magnify-labs/otel-magnify/pkg/ext"
	"github.com/magnify-labs/otel-magnify/pkg/models"
)

type meResponse struct {
	ID          string                 `json:"id"`
	Email       string                 `json:"email"`
	Groups      []models.Group         `json:"groups"`
	Preferences models.UserPreferences `json:"preferences"`
}

func (a *API) handleGetMe(w http.ResponseWriter, r *http.Request) {
	info := ext.UserInfoFromContext(r.Context())
	if info == nil {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	groups, err := a.db.GetUserGroups(info.UserID)
	if err != nil {
		respondError(w, 500, "failed to load groups")
		return
	}
	if groups == nil {
		groups = []models.Group{}
	}
	prefs, err := a.db.GetUserPreferences(info.UserID)
	if err != nil {
		respondError(w, 500, "failed to load preferences")
		return
	}
	respondJSON(w, 200, meResponse{
		ID:          info.UserID,
		Email:       info.Email,
		Groups:      groups,
		Preferences: prefs,
	})
}
