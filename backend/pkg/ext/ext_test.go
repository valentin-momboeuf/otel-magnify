package ext_test

import (
	"context"
	"testing"

	"otel-magnify/internal/store"
	"otel-magnify/pkg/ext"
)

// Compile-time interface satisfaction checks.
// Note: ext.AuthProvider check for *auth.Auth lives in internal/auth/auth_test.go.
// Note: ext.AlertNotifier check for *alerts.WebhookNotifier is temporarily removed
// because internal/alerts imports internal/api which still uses the old auth API.
// It will be restored once internal/api is refactored (Task 5).
var (
	_ ext.Store       = (*store.DB)(nil)
	_ ext.AuditLogger = ext.NopAuditLogger{}
)

func TestUserInfoContextRoundTrip(t *testing.T) {
	info := &ext.UserInfo{
		UserID: "u-123",
		Email:  "test@example.com",
		Role:   "admin",
	}

	ctx := ext.ContextWithUserInfo(context.Background(), info)
	got := ext.UserInfoFromContext(ctx)

	if got == nil {
		t.Fatal("expected UserInfo, got nil")
	}
	if got.UserID != info.UserID {
		t.Errorf("UserID = %q, want %q", got.UserID, info.UserID)
	}
	if got.Email != info.Email {
		t.Errorf("Email = %q, want %q", got.Email, info.Email)
	}
	if got.Role != info.Role {
		t.Errorf("Role = %q, want %q", got.Role, info.Role)
	}
}

func TestUserInfoFromEmptyContext(t *testing.T) {
	got := ext.UserInfoFromContext(context.Background())
	if got != nil {
		t.Errorf("expected nil from empty context, got %+v", got)
	}
}
