// Package auth provides JWT-based authentication and HTTP middleware.
package auth

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/magnify-labs/otel-magnify/pkg/ext"
)

// claims holds the JWT payload. Internal to this package.
type claims struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
	Role   string `json:"role"`
	jwt.RegisteredClaims
}

// Auth handles token generation and validation for a given HMAC secret.
type Auth struct {
	secret []byte
}

// New creates an Auth instance. The secret must be at least 32 bytes for
// adequate HMAC-SHA256 security.
func New(secret string) *Auth {
	return &Auth{secret: []byte(secret)}
}

// GenerateToken mints a signed JWT for the given user attributes.
// Tokens expire after 24 hours.
func (a *Auth) GenerateToken(userID, email, role string) (string, error) {
	c := claims{
		UserID: userID,
		Email:  email,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, c)
	return token.SignedString(a.secret)
}

// ValidateToken parses and verifies the token string, returning the embedded
// user info on success. It explicitly rejects tokens signed with a non-HMAC
// algorithm to prevent the "alg:none" attack.
func (a *Auth) ValidateToken(tokenStr string) (*ext.UserInfo, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &claims{}, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return a.secret, nil
	})
	if err != nil {
		return nil, err
	}
	c, ok := token.Claims.(*claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}
	return &ext.UserInfo{UserID: c.UserID, Email: c.Email, Role: c.Role}, nil
}

// Middleware returns an HTTP handler that enforces Bearer token authentication.
// On success it stores the validated UserInfo in the request context so downstream
// handlers can retrieve it via ext.UserInfoFromContext.
func (a *Auth) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if header == "" || !strings.HasPrefix(header, "Bearer ") {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		tokenStr := strings.TrimPrefix(header, "Bearer ")
		info, err := a.ValidateToken(tokenStr)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		ctx := ext.ContextWithUserInfo(r.Context(), info)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
