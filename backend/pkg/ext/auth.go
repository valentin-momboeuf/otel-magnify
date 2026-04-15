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
