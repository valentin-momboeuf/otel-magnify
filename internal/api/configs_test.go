package api

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/magnify-labs/otel-magnify/internal/auth"
	"github.com/magnify-labs/otel-magnify/pkg/models"
)

func TestCreateAndListConfigs(t *testing.T) {
	_, router, _ := newTestAPI(t)

	body := `{"name":"collector-base","content":"receivers:\n  otlp:"}`
	a := auth.New("test-secret-key-at-least-32-bytes!")
	token, _ := a.GenerateToken("user-001", "admin@test.com", "admin")

	req := httptest.NewRequest("POST", "/api/configs", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != 201 {
		t.Fatalf("create status = %d, body = %s", rec.Code, rec.Body.String())
	}

	req = authedRequest(t, "GET", "/api/configs")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("list status = %d", rec.Code)
	}

	var configs []models.Config
	json.NewDecoder(rec.Body).Decode(&configs)
	if len(configs) != 1 {
		t.Errorf("len = %d, want 1", len(configs))
	}
}

func TestLoginHandler(t *testing.T) {
	db, router, _ := newTestAPI(t)

	hash, _ := hashPassword("testpass123")
	db.CreateUser(models.User{
		ID: "user-001", Email: "admin@test.com", PasswordHash: hash, Role: "admin",
	})

	body := `{"email":"admin@test.com","password":"testpass123"}`
	req := httptest.NewRequest("POST", "/api/auth/login", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("login status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var resp map[string]string
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["token"] == "" {
		t.Error("expected non-empty token")
	}
}

func TestListAlerts_Handler(t *testing.T) {
	db, router, _ := newTestAPI(t)
	db.UpsertWorkload(models.Workload{
		ID: "w1", Type: "collector", Status: "connected",
		LastSeenAt: time.Now().UTC(), Labels: models.Labels{},
	})
	db.CreateAlert(models.Alert{
		ID: "alert-1", WorkloadID: "w1", Rule: "workload_down",
		Severity: "critical", Message: "down", FiredAt: time.Now().UTC(),
	})

	req := authedRequest(t, "GET", "/api/alerts")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}

	var alerts []models.Alert
	json.NewDecoder(rec.Body).Decode(&alerts)
	if len(alerts) != 1 {
		t.Errorf("len = %d, want 1", len(alerts))
	}
}
