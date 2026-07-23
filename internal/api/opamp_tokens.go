package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/magnify-labs/otel-magnify/internal/opampauth"
	"github.com/magnify-labs/otel-magnify/pkg/ext"
	"github.com/magnify-labs/otel-magnify/pkg/models"
)

const maxOpAMPTokenBodyBytes int64 = 8 << 10

type createOpAMPTokenRequest struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Team        string     `json:"team"`
	Environment string     `json:"environment"`
	ExpiresAt   *time.Time `json:"expires_at"`
}

type createOpAMPTokenResponse struct {
	Token models.OpAMPToken `json:"token"`
	Value string            `json:"value"`
}

type listOpAMPTokensResponse struct {
	Tokens []models.OpAMPToken `json:"tokens"`
}

type revokeOpAMPTokenResponse struct {
	Token                   models.OpAMPToken `json:"token"`
	DisconnectedConnections int               `json:"disconnected_connections"`
}

func (a *API) handleCreateOpAMPToken(w http.ResponseWriter, r *http.Request) {
	var request createOpAMPTokenRequest
	if !decodeOpAMPTokenJSONBody(w, r, &request) {
		return
	}

	now := time.Now().UTC()
	request.Name = strings.TrimSpace(request.Name)
	if err := validateCreateOpAMPTokenRequest(request, now); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	if request.ExpiresAt != nil {
		expiresAt := request.ExpiresAt.UTC()
		request.ExpiresAt = &expiresAt
	}

	generated, err := opampauth.Generate()
	if err != nil {
		respondError(w, http.StatusServiceUnavailable, "token generation unavailable")
		return
	}
	user := ext.UserInfoFromContext(r.Context())
	token := models.OpAMPToken{
		ID:          generated.ID,
		Name:        request.Name,
		Description: request.Description,
		Team:        request.Team,
		Environment: request.Environment,
		CreatedAt:   now,
		CreatedBy:   user.UserID,
		ExpiresAt:   request.ExpiresAt,
		Status:      models.OpAMPTokenActive,
	}
	event := newOpAMPTokenAuditEvent("opamp.token.create", token.ID, now, user)
	credential := models.OpAMPTokenCredential{Token: token, SecretHash: generated.SecretHash}
	if err := a.db.CreateOpAMPToken(r.Context(), credential, event); err != nil {
		if errors.Is(err, ext.ErrCommitOutcomeUnknown) {
			respondOpAMPTokenOutcomeUnknown(w, token.ID)
			return
		}
		respondError(w, http.StatusServiceUnavailable, "token store unavailable")
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	respondJSON(w, http.StatusCreated, createOpAMPTokenResponse{Token: token, Value: generated.Value})
}

func (a *API) handleListOpAMPTokens(w http.ResponseWriter, r *http.Request) {
	tokens, err := a.db.ListOpAMPTokens(r.Context(), time.Now().UTC())
	if err != nil {
		respondError(w, http.StatusServiceUnavailable, "token store unavailable")
		return
	}
	if tokens == nil {
		tokens = make([]models.OpAMPToken, 0)
	}
	respondJSON(w, http.StatusOK, listOpAMPTokensResponse{Tokens: tokens})
}

func (a *API) handleRevokeOpAMPToken(w http.ResponseWriter, r *http.Request) {
	tokenID := chi.URLParam(r, "id")
	now := time.Now().UTC()
	user := ext.UserInfoFromContext(r.Context())
	event := newOpAMPTokenAuditEvent("opamp.token.revoke", tokenID, now, user)

	token, _, err := a.db.RevokeOpAMPToken(r.Context(), tokenID, user.UserID, now, event)
	if err != nil {
		switch {
		case errors.Is(err, ext.ErrOpAMPTokenNotFound):
			respondError(w, http.StatusNotFound, "token not found")
		case errors.Is(err, ext.ErrCommitOutcomeUnknown):
			a.disconnectTokenConnections(tokenID)
			respondOpAMPTokenOutcomeUnknown(w, tokenID)
		default:
			respondError(w, http.StatusServiceUnavailable, "token store unavailable")
		}
		return
	}

	disconnected := a.disconnectTokenConnections(tokenID)
	respondJSON(w, http.StatusOK, revokeOpAMPTokenResponse{
		Token:                   token,
		DisconnectedConnections: disconnected,
	})
}

func (a *API) disconnectTokenConnections(tokenID string) int {
	if a.opamp == nil {
		return 0
	}
	return a.opamp.DisconnectTokenConnections(tokenID)
}

func decodeOpAMPTokenJSONBody(w http.ResponseWriter, r *http.Request, destination any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxOpAMPTokenBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		respondOpAMPTokenDecodeError(w, err)
		return false
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != nil {
		if errors.Is(err, io.EOF) {
			return true
		}
		respondOpAMPTokenDecodeError(w, err)
		return false
	}
	respondError(w, http.StatusBadRequest, "invalid JSON")
	return false
}

func respondOpAMPTokenDecodeError(w http.ResponseWriter, err error) {
	var maxBytesError *http.MaxBytesError
	if errors.As(err, &maxBytesError) {
		respondError(w, http.StatusRequestEntityTooLarge, "request body too large")
		return
	}
	respondError(w, http.StatusBadRequest, "invalid JSON")
}

func validateCreateOpAMPTokenRequest(request createOpAMPTokenRequest, now time.Time) error {
	switch {
	case utf8.RuneCountInString(request.Name) < 1:
		return errors.New("name is required")
	case utf8.RuneCountInString(request.Name) > 128:
		return errors.New("name exceeds 128 characters")
	case utf8.RuneCountInString(request.Description) > 512:
		return errors.New("description exceeds 512 characters")
	case utf8.RuneCountInString(request.Team) > 128:
		return errors.New("team exceeds 128 characters")
	case utf8.RuneCountInString(request.Environment) > 128:
		return errors.New("environment exceeds 128 characters")
	case request.ExpiresAt != nil && !request.ExpiresAt.UTC().After(now):
		return errors.New("expires_at must be in the future")
	default:
		return nil
	}
}

func newOpAMPTokenAuditEvent(action, tokenID string, occurredAt time.Time, user *ext.UserInfo) ext.AuditEvent {
	eventID := uuid.NewString()
	for eventID == tokenID {
		eventID = uuid.NewString()
	}
	return ext.AuditEvent{
		EventID:    eventID,
		OccurredAt: occurredAt,
		Action:     action,
		UserID:     user.UserID,
		Email:      user.Email,
		Resource:   "opamp_token",
		ResourceID: tokenID,
		Detail:     "",
	}
}

func respondOpAMPTokenOutcomeUnknown(w http.ResponseWriter, tokenID string) {
	respondJSON(w, http.StatusServiceUnavailable, map[string]string{
		"error":              "operation outcome unknown",
		"side_effect_status": string(sideEffectUnknown),
		"token_id":           tokenID,
	})
}
