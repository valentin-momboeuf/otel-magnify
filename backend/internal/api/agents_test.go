package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"otel-magnify/internal/auth"
	"otel-magnify/internal/store"
	"otel-magnify/pkg/models"
)

type fakeOpAMP struct {
	pushed [][]byte
	err    error
}

func (f *fakeOpAMP) PushConfig(_ context.Context, _ string, y []byte) error {
	f.pushed = append(f.pushed, y)
	return f.err
}

func newTestAPI(t *testing.T) (*store.DB, http.Handler, *fakeOpAMP) {
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

	opampFake := &fakeOpAMP{}
	router := NewRouter(db, a, hub, opampFake, "", nil)
	return db, router, opampFake
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
	_, router, _ := newTestAPI(t)
	req := authedRequest(t, "GET", "/api/agents")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestListAgents_WithData(t *testing.T) {
	db, router, _ := newTestAPI(t)

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
	db, router, _ := newTestAPI(t)
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
	_, router, _ := newTestAPI(t)

	req := authedRequest(t, "GET", "/api/agents/nonexistent")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != 404 {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestHandlePushConfig_PersistsAndReturnsHash(t *testing.T) {
	db, router, opampFake := newTestAPI(t)
	_ = db.UpsertAgent(models.Agent{
		ID: "a1", Type: "collector", Status: "connected",
		LastSeenAt: time.Now().UTC(), Labels: models.Labels{},
		AcceptsRemoteConfig: true,
	})

	a := auth.New("test-secret-key-at-least-32-bytes!")
	token, _ := a.GenerateToken("user-001", "admin@test.com", "admin")
	validYAML := `receivers:
  otlp: {}
exporters:
  logging: {}
service:
  pipelines:
    traces:
      receivers: [otlp]
      exporters: [logging]
`
	req := httptest.NewRequest("POST", "/api/agents/a1/config", strings.NewReader(validYAML))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "text/yaml")

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != 202 {
		t.Fatalf("status %d, body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if len(body["config_hash"]) != 64 {
		t.Fatalf("expected 64-char hex hash, got %q", body["config_hash"])
	}
	hist, _ := db.GetAgentConfigHistory("a1")
	if len(hist) != 1 || hist[0].Status != "pending" || hist[0].PushedBy != "admin@test.com" {
		t.Fatalf("history not recorded: %+v", hist)
	}
	if len(opampFake.pushed) != 1 {
		t.Fatalf("expected 1 opamp push, got %d", len(opampFake.pushed))
	}
}

func TestHandlePushConfig_RejectsEmptyBody(t *testing.T) {
	db, router, _ := newTestAPI(t)
	_ = db.UpsertAgent(models.Agent{ID: "a1", Type: "collector", Status: "connected", LastSeenAt: time.Now().UTC(), Labels: models.Labels{}})

	a := auth.New("test-secret-key-at-least-32-bytes!")
	token, _ := a.GenerateToken("user-001", "admin@test.com", "admin")
	req := httptest.NewRequest("POST", "/api/agents/a1/config", strings.NewReader(""))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "text/yaml")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != 400 {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestHandleValidateConfig_ReturnsErrorsForBadYAML(t *testing.T) {
	db, router, _ := newTestAPI(t)
	_ = db.UpsertAgent(models.Agent{ID: "a1", Type: "collector", Status: "connected", LastSeenAt: time.Now().UTC(), Labels: models.Labels{}})

	a := auth.New("test-secret-key-at-least-32-bytes!")
	token, _ := a.GenerateToken("user-001", "admin@test.com", "admin")
	req := httptest.NewRequest("POST", "/api/agents/a1/config/validate", strings.NewReader("receivers: {}"))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "text/yaml")

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	var result struct {
		Valid  bool `json:"valid"`
		Errors []struct {
			Code string `json:"code"`
		} `json:"errors"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &result)
	if result.Valid || len(result.Errors) == 0 {
		t.Fatalf("expected validation errors, got %+v", result)
	}
}

func TestHandleValidateConfig_UsesAgentAvailableComponents(t *testing.T) {
	db, router, _ := newTestAPI(t)
	_ = db.UpsertAgent(models.Agent{
		ID: "a1", Type: "collector", Status: "connected", LastSeenAt: time.Now().UTC(), Labels: models.Labels{},
		AvailableComponents: &models.AvailableComponents{
			Components: map[string][]string{
				"receivers": {"otlp"},
				"exporters": {"logging"},
			},
		},
	})

	yamlWithUnknownReceiver := `receivers:
  jaeger: {}
exporters:
  logging: {}
service:
  pipelines:
    traces:
      receivers: [jaeger]
      exporters: [logging]
`
	a := auth.New("test-secret-key-at-least-32-bytes!")
	token, _ := a.GenerateToken("user-001", "admin@test.com", "admin")
	req := httptest.NewRequest("POST", "/api/agents/a1/config/validate", strings.NewReader(yamlWithUnknownReceiver))
	req.Header.Set("Authorization", "Bearer "+token)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	var result struct {
		Valid  bool `json:"valid"`
		Errors []struct {
			Code string `json:"code"`
		} `json:"errors"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &result)
	if result.Valid {
		t.Fatal("expected invalid (jaeger not in available components)")
	}
	foundNotInstalled := false
	for _, e := range result.Errors {
		if e.Code == "component_not_installed" {
			foundNotInstalled = true
		}
	}
	if !foundNotInstalled {
		t.Errorf("expected component_not_installed error, got %+v", result.Errors)
	}
}

func TestHandlePushConfig_RejectsInvalidYAML(t *testing.T) {
	db, router, opampFake := newTestAPI(t)
	_ = db.UpsertAgent(models.Agent{
		ID: "a1", Type: "collector", Status: "connected",
		LastSeenAt: time.Now().UTC(), Labels: models.Labels{},
		AcceptsRemoteConfig: true,
	})

	a := auth.New("test-secret-key-at-least-32-bytes!")
	token, _ := a.GenerateToken("user-001", "admin@test.com", "admin")
	req := httptest.NewRequest("POST", "/api/agents/a1/config", strings.NewReader("receivers: {}"))
	req.Header.Set("Authorization", "Bearer "+token)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != 400 {
		t.Fatalf("expected 400 (validation rejection), got %d: %s", rec.Code, rec.Body.String())
	}
	if len(opampFake.pushed) != 0 {
		t.Errorf("opamp push should not have been called on invalid config")
	}
}

func TestHandlePushConfig_RejectsWhenRemoteConfigNotAccepted(t *testing.T) {
	db, router, opampFake := newTestAPI(t)
	_ = db.UpsertAgent(models.Agent{
		ID: "a-ro", Type: "collector", Status: "connected",
		LastSeenAt: time.Now().UTC(), Labels: models.Labels{},
		AcceptsRemoteConfig: false,
	})

	a := auth.New("test-secret-key-at-least-32-bytes!")
	token, _ := a.GenerateToken("user-001", "admin@test.com", "admin")

	validYAML := `receivers:
  otlp: {}
exporters:
  logging: {}
service:
  pipelines:
    traces:
      receivers: [otlp]
      exporters: [logging]
`
	req := httptest.NewRequest("POST", "/api/agents/a-ro/config", strings.NewReader(validYAML))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "text/yaml")

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409, body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["code"] != "remote_config_unsupported" {
		t.Fatalf("code = %q, want %q", body["code"], "remote_config_unsupported")
	}
	// Guard runs before the OpAMP push: nothing should have been sent to the agent.
	if len(opampFake.pushed) != 0 {
		t.Fatalf("expected 0 opamp pushes, got %d", len(opampFake.pushed))
	}

	// Flip the flag to true and push must now succeed (regression guard).
	_ = db.UpsertAgent(models.Agent{
		ID: "a-ro", Type: "collector", Status: "connected",
		LastSeenAt: time.Now().UTC(), Labels: models.Labels{},
		AcceptsRemoteConfig: true,
	})
	req = httptest.NewRequest("POST", "/api/agents/a-ro/config", strings.NewReader(validYAML))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "text/yaml")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != 202 {
		t.Fatalf("status = %d after flip-on, want 202, body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleGetAgentConfigHistory_IncludesErrorAndContent(t *testing.T) {
	db, router, _ := newTestAPI(t)
	_ = db.UpsertAgent(models.Agent{ID: "a1", Type: "collector", Status: "connected", LastSeenAt: time.Now().UTC(), Labels: models.Labels{}})
	_ = db.CreateConfig(models.Config{ID: "c1", Name: "n", Content: "my-yaml", CreatedAt: time.Now().UTC()})
	_ = db.RecordAgentConfig(models.AgentConfig{AgentID: "a1", ConfigID: "c1", Status: "failed", ErrorMessage: "oops", PushedBy: "u@x"})

	req := authedRequest(t, "GET", "/api/agents/a1/configs")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	var hist []models.AgentConfig
	_ = json.Unmarshal(rec.Body.Bytes(), &hist)
	if len(hist) != 1 || hist[0].ErrorMessage != "oops" || hist[0].Content != "my-yaml" || hist[0].PushedBy != "u@x" {
		t.Fatalf("history shape: %+v", hist)
	}
}
