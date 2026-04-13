// Package auth provides JWT-based authentication and HTTP middleware.
package auth

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// contextKey is an unexported type to avoid context key collisions across packages.
type contextKey struct{}

// Claims holds the JWT payload fields we care about, embedded alongside
// the standard registered claims (exp, iat, etc.).
type Claims struct {
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
	claims := Claims{
		UserID: userID,
		Email:  email,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(a.secret)
}

// ValidateToken parses and verifies the token string, returning the embedded
// claims on success. It explicitly rejects tokens signed with a non-HMAC
// algorithm to prevent the "alg:none" attack.
func (a *Auth) ValidateToken(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return a.secret, nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}
	return claims, nil
}

// Middleware returns an HTTP handler that enforces Bearer token authentication.
// On success it stores the validated Claims in the request context so downstream
// handlers can retrieve them via ClaimsFromContext.
func (a *Auth) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if header == "" || !strings.HasPrefix(header, "Bearer ") {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		tokenStr := strings.TrimPrefix(header, "Bearer ")
		claims, err := a.ValidateToken(tokenStr)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), contextKey{}, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ClaimsFromContext retrieves the Claims stored by Middleware.
// Returns nil if no claims are present (e.g. unauthenticated route).
func ClaimsFromContext(ctx context.Context) *Claims {
	claims, _ := ctx.Value(contextKey{}).(*Claims)
	return claims
}
