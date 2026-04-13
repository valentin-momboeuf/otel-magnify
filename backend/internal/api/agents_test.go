package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"otel-magnify/internal/auth"
	"otel-magnify/internal/store"
	"otel-magnify/pkg/models"
)

func newTestAPI(t *testing.T) (*store.DB, http.Handler) {
	t.Helper()
	db, err := store.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	a := auth.New("test-secret-key-at-least-32-bytes!")
	hub := NewHub()
	go hub.Run()
	t.Cleanup(hub.Stop)

	router := NewRouter(db, a, hub, nil, "", nil)
	return db, router
}

func authedRequest(t *testing.T, method, url string) *http.Request {
	t.Helper()
	a := auth.New("test-secret-key-at-least-32-bytes!")
	token, _ := a.GenerateToken("user-001", "admin@test.com", "admin")
	req := httptest.NewRequest(method, url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}

func TestListAgents_Empty(t *testing.T) {
	_, router := newTestAPI(t)
	req := authedRequest(t, "GET", "/api/agents")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestListAgents_WithData(t *testing.T) {
	db, router := newTestAPI(t)

	db.UpsertAgent(models.Agent{
		ID: "a1", DisplayName: "test", Type: "collector",
		Status: "connected", LastSeenAt: time.Now().UTC(), Labels: models.Labels{},
	})

	req := authedRequest(t, "GET", "/api/agents")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var agents []models.Agent
	json.NewDecoder(rec.Body).Decode(&agents)
	if len(agents) != 1 {
		t.Errorf("len = %d, want 1", len(agents))
	}
}

func TestGetAgent(t *testing.T) {
	db, router := newTestAPI(t)
	db.UpsertAgent(models.Agent{
		ID: "a1", DisplayName: "test", Type: "collector",
		Status: "connected", LastSeenAt: time.Now().UTC(), Labels: models.Labels{},
	})

	req := authedRequest(t, "GET", "/api/agents/a1")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var agent models.Agent
	json.NewDecoder(rec.Body).Decode(&agent)
	if agent.ID != "a1" {
		t.Errorf("ID = %q, want a1", agent.ID)
	}
}

func TestGetAgent_NotFound(t *testing.T) {
	_, router := newTestAPI(t)

	req := authedRequest(t, "GET", "/api/agents/nonexistent")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != 404 {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}
