package ext

import "context"

// AuditEvent describes a single security-relevant action recorded by an AuditLogger.
type AuditEvent struct {
	Action     string
	UserID     string
	Email      string
	Resource   string
	ResourceID string
	Detail     string
}

// AuditLogger sinks AuditEvents to the configured backend (file, syslog, SIEM, etc.).
type AuditLogger interface {
	Log(ctx context.Context, event AuditEvent)
}

// NopAuditLogger is a no-op AuditLogger used as the default when no audit sink is wired.
type NopAuditLogger struct{}

// Log discards the event.
func (NopAuditLogger) Log(context.Context, AuditEvent) {}
