package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/magnify-labs/otel-magnify/internal/store"
	"github.com/magnify-labs/otel-magnify/pkg/models"
	"golang.org/x/crypto/bcrypt"
)

func seedUserWithPassword(t *testing.T, db *store.DB, id, email, pw string) {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(pw), 12)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if err := db.CreateUser(models.User{
		ID: id, Email: email, PasswordHash: string(hash),
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := db.AttachUserToGroupByName(id, "viewer"); err != nil {
		t.Fatalf("attach: %v", err)
	}
}

func TestHandlePutPassword_Success(t *testing.T) {
	db, auth := newMeTestAPI(t)
	seedUserWithPassword(t, db, "u1", "u1@x.com", "oldpassword!!12")

	tok, _ := auth.GenerateToken("u1", "u1@x.com", []string{"viewer"})
	body, _ := json.Marshal(map[string]string{
		"current_password": "oldpassword!!12",
		"new_password":     "newpassword!!!12",
	})
	req := httptest.NewRequest(http.MethodPut, "/api/me/password", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	buildMeTestRouter(db, auth).ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	u, _ := db.GetUserByEmail("u1@x.com")
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte("newpassword!!!12")) != nil {
		t.Error("new password not persisted")
	}
}

func TestHandlePutPassword_WrongCurrent(t *testing.T) {
	db, auth := newMeTestAPI(t)
	seedUserWithPassword(t, db, "u1", "u1@x.com", "rightpassword12")

	tok, _ := auth.GenerateToken("u1", "u1@x.com", []string{"viewer"})
	body, _ := json.Marshal(map[string]string{
		"current_password": "WRONGpassword12",
		"new_password":     "newpassword!!!12",
	})
	req := httptest.NewRequest(http.MethodPut, "/api/me/password", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	buildMeTestRouter(db, auth).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", rec.Code)
	}
}

func TestHandlePutPassword_TooShort(t *testing.T) {
	db, auth := newMeTestAPI(t)
	seedUserWithPassword(t, db, "u1", "u1@x.com", "rightpassword12")

	tok, _ := auth.GenerateToken("u1", "u1@x.com", []string{"viewer"})
	body, _ := json.Marshal(map[string]string{
		"current_password": "rightpassword12",
		"new_password":     "short",
	})
	req := httptest.NewRequest(http.MethodPut, "/api/me/password", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	buildMeTestRouter(db, auth).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", rec.Code)
	}
}

func TestHandlePutPassword_SameAsCurrent(t *testing.T) {
	db, auth := newMeTestAPI(t)
	seedUserWithPassword(t, db, "u1", "u1@x.com", "samepassword123")

	tok, _ := auth.GenerateToken("u1", "u1@x.com", []string{"viewer"})
	body, _ := json.Marshal(map[string]string{
		"current_password": "samepassword123",
		"new_password":     "samepassword123",
	})
	req := httptest.NewRequest(http.MethodPut, "/api/me/password", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	buildMeTestRouter(db, auth).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", rec.Code)
	}
}
