package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGenerateAndValidateToken(t *testing.T) {
	a := New("test-secret-key-at-least-32-bytes!")

	token, err := a.GenerateToken("user-001", "admin@test.com", "admin")
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	claims, err := a.ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
	if claims.UserID != "user-001" {
		t.Errorf("UserID = %q, want user-001", claims.UserID)
	}
	if claims.Email != "admin@test.com" {
		t.Errorf("Email = %q, want admin@test.com", claims.Email)
	}
	if claims.Role != "admin" {
		t.Errorf("Role = %q, want admin", claims.Role)
	}
}

func TestValidateToken_Invalid(t *testing.T) {
	a := New("test-secret-key-at-least-32-bytes!")
	_, err := a.ValidateToken("garbage.token.here")
	if err == nil {
		t.Error("expected error for invalid token")
	}
}

func TestMiddleware_NoToken(t *testing.T) {
	a := New("test-secret-key-at-least-32-bytes!")

	handler := a.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("GET", "/api/agents", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestMiddleware_ValidToken(t *testing.T) {
	a := New("test-secret-key-at-least-32-bytes!")

	token, _ := a.GenerateToken("user-001", "admin@test.com", "admin")

	handler := a.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims := ClaimsFromContext(r.Context())
		if claims == nil {
			t.Error("expected claims in context")
		}
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("GET", "/api/agents", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}
