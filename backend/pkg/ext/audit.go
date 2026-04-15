package ext

import "context"

type AuditEvent struct {
	Action     string
	UserID     string
	Email      string
	Resource   string
	ResourceID string
	Detail     string
}

type AuditLogger interface {
	Log(ctx context.Context, event AuditEvent)
}

type NopAuditLogger struct{}

func (NopAuditLogger) Log(context.Context, AuditEvent) {}
