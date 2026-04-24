package server

import (
	"io/fs"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/magnify-labs/otel-magnify/pkg/ext"
)

// Config holds the server's listen addresses and feature flags.
type Config struct {
	ListenAddr      string // default ":8080"
	OpAMPAddr       string // default ":4320"
	CORSOrigins     string
	MinAgentVersion string

	// Workload lifecycle tuning. Zero values let the downstream subsystems
	// apply their own defaults (2-minute grace, 30-day retention, 5-minute
	// janitor tick).
	WorkloadRetention       time.Duration
	WorkloadDisconnectGrace time.Duration
	WorkloadJanitorInterval time.Duration
	WorkloadEventRetention  time.Duration
}

// Option configures optional features on the Server.
type Option func(*Server)

// WithNotifier adds an AlertNotifier (webhook, email, Slack, etc.).
func WithNotifier(n ext.AlertNotifier) Option {
	return func(s *Server) {
		s.notifiers = append(s.notifiers, n)
	}
}

// WithAuditLogger sets the audit logger. Default is ext.NopAuditLogger.
func WithAuditLogger(l ext.AuditLogger) Option {
	return func(s *Server) {
		s.auditLogger = l
	}
}

// WithStaticFS sets the embedded frontend assets for SPA serving.
func WithStaticFS(fsys fs.FS) Option {
	return func(s *Server) {
		s.staticFS = fsys
	}
}

// WithRouterHook adds a function that can modify the chi router before
// the server starts. Use this to add middleware (RBAC, audit) or extra routes.
func WithRouterHook(fn func(chi.Router)) Option {
	return func(s *Server) {
		s.routerHooks = append(s.routerHooks, fn)
	}
}

// WithAuthMethod registers an additional login method exposed on
// GET /api/auth/methods. The "password" method is always registered by
// default. Duplicate IDs are ignored with a warning log; the first
// registration wins (so the built-in default cannot be accidentally
// overridden by an enterprise option).
func WithAuthMethod(m ext.AuthMethod) Option {
	return func(s *Server) {
		s.authMethods = append(s.authMethods, m)
	}
}

// WithAuthMethodProvider registers a callback consulted on every
// GET /api/auth/methods request. When set, the provider's return value
// fully replaces the static list built from the default "password"
// method plus any WithAuthMethod(...) entries. The fallback to the
// static list applies when no provider is registered or the provider
// itself returns nil.
//
// Enterprise binaries use this to serve a DB-backed, runtime-mutable
// list of SSO providers without restarting.
func WithAuthMethodProvider(fn func() []ext.AuthMethod) Option {
	return func(s *Server) {
		s.authMethodProvider = fn
	}
}
