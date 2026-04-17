package server

import (
	"io/fs"

	"github.com/go-chi/chi/v5"

	"otel-magnify/pkg/ext"
)

// Config holds the server's listen addresses and feature flags.
type Config struct {
	ListenAddr      string // default ":8080"
	OpAMPAddr       string // default ":4320"
	CORSOrigins     string
	MinAgentVersion string
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
