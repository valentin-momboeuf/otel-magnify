package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"otel-magnify/internal/auth"
	"otel-magnify/internal/opamp"
	"otel-magnify/internal/store"
)

type API struct {
	db    *store.DB
	auth  *auth.Auth
	hub   *Hub
	opamp *opamp.Server
}

func NewRouter(db *store.DB, a *auth.Auth, hub *Hub, opampSrv *opamp.Server) http.Handler {
	api := &API{db: db, auth: a, hub: hub, opamp: opampSrv}

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// Public routes
	r.Post("/api/auth/login", api.handleLogin)

	// Protected routes
	r.Group(func(r chi.Router) {
		r.Use(a.Middleware)

		r.Get("/api/agents", api.handleListAgents)
		r.Get("/api/agents/{id}", api.handleGetAgent)
		r.Post("/api/agents/{id}/config", api.handlePushConfig)

		r.Get("/api/configs", api.handleListConfigs)
		r.Post("/api/configs", api.handleCreateConfig)
		r.Get("/api/configs/{id}", api.handleGetConfig)

		r.Get("/api/alerts", api.handleListAlerts)
		r.Post("/api/alerts/{id}/resolve", api.handleResolveAlert)
	})

	// WebSocket for frontend
	r.Get("/ws", hub.HandleWS)

	return r
}

func respondJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func respondError(w http.ResponseWriter, status int, msg string) {
	respondJSON(w, status, map[string]string{"error": msg})
}
