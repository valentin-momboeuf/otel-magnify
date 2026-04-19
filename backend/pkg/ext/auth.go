// Package ext defines the extension interfaces for the otel-magnify module overlay.
// Enterprise builds import this package to implement custom providers.
package ext

import (
	"context"
	"net/http"
)

type userInfoKey struct{}

type UserInfo struct {
	UserID string
	Email  string
	Role   string
}

type AuthProvider interface {
	GenerateToken(userID, email, role string) (string, error)
	ValidateToken(tokenStr string) (*UserInfo, error)
	Middleware(next http.Handler) http.Handler
}

func UserInfoFromContext(ctx context.Context) *UserInfo {
	info, _ := ctx.Value(userInfoKey{}).(*UserInfo)
	return info
}

func ContextWithUserInfo(ctx context.Context, info *UserInfo) context.Context {
	return context.WithValue(ctx, userInfoKey{}, info)
}

// AuthMethod describes a login method advertised to the frontend so it
// can render a "Sign in with X" button or the password form.
type AuthMethod struct {
	ID          string `json:"id"`           // e.g., "password" | "okta-main"
	Type        string `json:"type"`         // "password" | "sso"
	DisplayName string `json:"display_name"` // e.g., "Okta Corporate"
	LoginURL    string `json:"login_url"`    // where the browser navigates to start the flow
}
