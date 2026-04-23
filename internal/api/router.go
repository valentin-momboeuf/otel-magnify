package api

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/magnify-labs/otel-magnify/internal/opamp"
	"github.com/magnify-labs/otel-magnify/internal/perm"
	"github.com/magnify-labs/otel-magnify/pkg/ext"
)

// OpAMPPusher is the subset of opamp.Server the HTTP layer uses.
// Declared here so handlers can be tested with a fake.
type OpAMPPusher interface {
	PushConfig(ctx context.Context, workloadID string, yamlContent []byte, targetInstanceUID string) error
	Instances(workloadID string) []opamp.Instance
}

type API struct {
	db                ext.Store
	auth              ext.AuthProvider
	hub               *Hub
	opamp             OpAMPPusher
	authMethods       []ext.AuthMethod
	workloadRetention time.Duration
}

func NewRouter(db ext.Store, a ext.AuthProvider, hub *Hub, opampSrv OpAMPPusher, corsOrigins string, staticFS fs.FS, authMethods []ext.AuthMethod, workloadRetention time.Duration) http.Handler {
	api := &API{db: db, auth: a, hub: hub, opamp: opampSrv, authMethods: authMethods, workloadRetention: workloadRetention}

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
	r.Get("/api/auth/methods", api.handleListAuthMethods)

	// WebSocket validates its own token via ?token= query param
	// (browsers cannot set Authorization headers on WS handshakes, so it
	// cannot live behind the Bearer-token middleware).
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

	// Protected routes
	r.Group(func(r chi.Router) {
		r.Use(a.Middleware)

		r.Get("/api/workloads", api.handleListWorkloads)
		r.Get("/api/workloads/{id}", api.handleGetWorkload)
		r.Get("/api/workloads/{id}/instances", api.handleListWorkloadInstances)
		r.Get("/api/workloads/{id}/events", api.handleListWorkloadEvents)
		r.Get("/api/workloads/{id}/events/stats", api.handleWorkloadEventsStats)
		r.With(api.RequirePerm(perm.PushConfig)).Post("/api/workloads/{id}/config", api.handlePushWorkloadConfig)
		r.With(api.RequirePerm(perm.ValidateConfig)).Post("/api/workloads/{id}/config/validate", api.handleValidateWorkloadConfig)
		r.Get("/api/workloads/{id}/configs", api.handleGetWorkloadConfigHistory)
		r.With(api.RequirePerm(perm.ArchiveWorkload)).Post("/api/workloads/{id}/archive", api.handleArchiveWorkload)
		r.With(api.RequirePerm(perm.DeleteWorkload)).Delete("/api/workloads/{id}", api.handleDeleteWorkload)

		// Legacy /api/agents/... redirects (remove at next minor release).
		r.Get("/api/agents", redirectAgentsToWorkloads)
		r.Get("/api/agents/{id}", redirectAgentsToWorkloads)
		r.Get("/api/agents/{id}/configs", redirectAgentsToWorkloads)
		r.Post("/api/agents/{id}/config", redirectAgentsToWorkloads)
		r.Post("/api/agents/{id}/config/validate", redirectAgentsToWorkloads)

		r.Get("/api/configs", api.handleListConfigs)
		r.With(api.RequirePerm(perm.CreateConfigTpl)).Post("/api/configs", api.handleCreateConfig)
		r.Get("/api/configs/{id}", api.handleGetConfig)

		r.Get("/api/alerts", api.handleListAlerts)
		r.With(api.RequirePerm(perm.ResolveAlert)).Post("/api/alerts/{id}/resolve", api.handleResolveAlert)

		r.Get("/api/pushes/activity", api.handleListPushActivity)

		r.Get("/api/me", api.handleGetMe)
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
