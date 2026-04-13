package api

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

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

func NewRouter(db *store.DB, a *auth.Auth, hub *Hub, opampSrv *opamp.Server, corsOrigins string, staticFS fs.FS) http.Handler {
	api := &API{db: db, auth: a, hub: hub, opamp: opampSrv}

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// CORS middleware
	allowedOrigins := []string{"http://localhost:5173"}
	if corsOrigins != "" {
		allowedOrigins = strings.Split(corsOrigins, ",")
	}
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   allowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Health check (public, no auth)
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	// Public routes
	r.Post("/api/auth/login", api.handleLogin)

	// Protected routes
	r.Group(func(r chi.Router) {
		r.Use(a.Middleware)

		r.Get("/api/agents", api.handleListAgents)
		r.Get("/api/agents/{id}", api.handleGetAgent)
		r.Post("/api/agents/{id}/config", api.handlePushConfig)
		r.Get("/api/agents/{id}/configs", api.handleGetAgentConfigHistory)

		r.Get("/api/configs", api.handleListConfigs)
		r.Post("/api/configs", api.handleCreateConfig)
		r.Get("/api/configs/{id}", api.handleGetConfig)

		r.Get("/api/alerts", api.handleListAlerts)
		r.Post("/api/alerts/{id}/resolve", api.handleResolveAlert)

		// WebSocket with token validation via query parameter
		r.Get("/ws", func(w http.ResponseWriter, r *http.Request) {
			token := r.URL.Query().Get("token")
			if token == "" {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			if _, err := a.ValidateToken(token); err != nil {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			hub.HandleWS(w, r)
		})
	})

	// Serve embedded frontend assets as catch-all (SPA fallback)
	if staticFS != nil {
		r.NotFound(ServeStatic(staticFS))
	}

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
