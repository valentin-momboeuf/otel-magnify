package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/magnify-labs/otel-magnify/internal/perm"
)

func TestRequirePerm_DeniesWhenMissing(t *testing.T) {
	db, auth := newMeTestAPI(t)
	api := &API{db: db, auth: auth}
	h := api.RequirePerm(perm.PushConfig)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	tok, _ := auth.GenerateToken("u1", "u1@x.com", []string{"viewer"})
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	auth.Middleware(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d, want 403", rec.Code)
	}
}

func TestRequirePerm_AllowsWhenPresent(t *testing.T) {
	db, auth := newMeTestAPI(t)
	api := &API{db: db, auth: auth}
	h := api.RequirePerm(perm.PushConfig)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	tok, _ := auth.GenerateToken("u1", "u1@x.com", []string{"editor"})
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	auth.Middleware(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", rec.Code)
	}
}
