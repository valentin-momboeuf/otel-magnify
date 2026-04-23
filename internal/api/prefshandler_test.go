package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandlePutPreferences_Success(t *testing.T) {
	db, auth := newMeTestAPI(t)
	seedUserWithPassword(t, db, "u1", "u1@x.com", "anypassword!!!12")

	tok, _ := auth.GenerateToken("u1", "u1@x.com", []string{"viewer"})
	body, _ := json.Marshal(map[string]string{"theme": "dark", "language": "fr"})
	req := httptest.NewRequest(http.MethodPut, "/api/me/preferences", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	buildMeTestRouter(db, auth).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		Theme    string `json:"theme"`
		Language string `json:"language"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out.Theme != "dark" || out.Language != "fr" {
		t.Errorf("response mismatch: %+v", out)
	}

	prefs, _ := db.GetUserPreferences("u1")
	if prefs.Theme != "dark" || prefs.Language != "fr" {
		t.Errorf("persisted mismatch: %+v", prefs)
	}
}

func TestHandlePutPreferences_InvalidTheme(t *testing.T) {
	db, auth := newMeTestAPI(t)
	seedUserWithPassword(t, db, "u1", "u1@x.com", "anypassword!!!12")

	tok, _ := auth.GenerateToken("u1", "u1@x.com", []string{"viewer"})
	body, _ := json.Marshal(map[string]string{"theme": "neon", "language": "en"})
	req := httptest.NewRequest(http.MethodPut, "/api/me/preferences", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	buildMeTestRouter(db, auth).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", rec.Code)
	}
}
