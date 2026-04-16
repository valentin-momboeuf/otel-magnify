package store

import (
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	"otel-magnify/pkg/models"
)

func TestCreateConfig(t *testing.T) {
	db := newTestDB(t)

	content := "receivers:\n  otlp:\n    protocols:\n      grpc:"
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(content)))

	cfg := models.Config{
		ID:        hash,
		Name:      "collector-base",
		Content:   content,
		CreatedAt: time.Now().UTC().Truncate(time.Second),
		CreatedBy: "admin@test.com",
	}

	if err := db.CreateConfig(cfg); err != nil {
		t.Fatalf("CreateConfig: %v", err)
	}

	got, err := db.GetConfig(hash)
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if got.Name != "collector-base" {
		t.Errorf("Name = %q, want collector-base", got.Name)
	}
	if got.Content != content {
		t.Errorf("Content mismatch")
	}
}

func TestListConfigs(t *testing.T) {
	db := newTestDB(t)

	for i := range 3 {
		content := fmt.Sprintf("config-%d", i)
		hash := fmt.Sprintf("%x", sha256.Sum256([]byte(content)))
		err := db.CreateConfig(models.Config{
			ID: hash, Name: fmt.Sprintf("cfg-%d", i), Content: content,
			CreatedAt: time.Now().UTC(), CreatedBy: "test",
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	configs, err := db.ListConfigs()
	if err != nil {
		t.Fatal(err)
	}
	if len(configs) != 3 {
		t.Errorf("len = %d, want 3", len(configs))
	}
}

func TestRecordAgentConfig(t *testing.T) {
	db := newTestDB(t)

	err := db.CreateConfig(models.Config{
		ID: "cfg-1", Name: "test", Content: "yaml", CreatedAt: time.Now().UTC(), CreatedBy: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	err = db.UpsertAgent(models.Agent{
		ID: "a1", Type: "collector", Status: "connected",
		LastSeenAt: time.Now().UTC(), Labels: models.Labels{},
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := db.RecordAgentConfig(models.AgentConfig{AgentID: "a1", ConfigID: "cfg-1", Status: "pending"}); err != nil {
		t.Fatalf("RecordAgentConfig: %v", err)
	}

	history, err := db.GetAgentConfigHistory("a1")
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 {
		t.Fatalf("len = %d, want 1", len(history))
	}
	if history[0].Status != "pending" {
		t.Errorf("Status = %q, want pending", history[0].Status)
	}
}

func TestRecordAgentConfig_WithPushedByAndError(t *testing.T) {
	db := newTestDB(t)
	seedAgent(t, db, "a1")
	seedConfig(t, db, "c1", "receivers: {}")

	if err := db.RecordAgentConfig(models.AgentConfig{
		AgentID:  "a1",
		ConfigID: "c1",
		Status:   "pending",
		PushedBy: "admin@magnify.dev",
	}); err != nil {
		t.Fatal(err)
	}

	hist, err := db.GetAgentConfigHistory("a1")
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 1 || hist[0].PushedBy != "admin@magnify.dev" {
		t.Fatalf("unexpected history: %+v", hist)
	}
	if hist[0].Content != "receivers: {}" {
		t.Fatalf("expected JOINed content, got %q", hist[0].Content)
	}
}

func TestUpdateAgentConfigStatus_SetsFailedWithError(t *testing.T) {
	db := newTestDB(t)
	seedAgent(t, db, "a1")
	seedConfig(t, db, "c1", "x")
	_ = db.RecordAgentConfig(models.AgentConfig{AgentID: "a1", ConfigID: "c1", Status: "pending"})

	if err := db.UpdateAgentConfigStatus("a1", "c1", "failed", "unknown exporter 'xyz'"); err != nil {
		t.Fatal(err)
	}

	hist, _ := db.GetAgentConfigHistory("a1")
	if hist[0].Status != "failed" || hist[0].ErrorMessage != "unknown exporter 'xyz'" {
		t.Fatalf("status/error not updated: %+v", hist[0])
	}
}

func TestUpdateAgentConfigStatus_AcceptsApplying(t *testing.T) {
	db := newTestDB(t)
	seedAgent(t, db, "a1")
	seedConfig(t, db, "c1", "x")
	_ = db.RecordAgentConfig(models.AgentConfig{AgentID: "a1", ConfigID: "c1", Status: "pending"})

	if err := db.UpdateAgentConfigStatus("a1", "c1", "applying", ""); err != nil {
		t.Fatalf("applying should be a valid status: %v", err)
	}

	hist, _ := db.GetAgentConfigHistory("a1")
	if hist[0].Status != "applying" {
		t.Fatalf("expected applying, got %q", hist[0].Status)
	}
}

func TestGetLastAppliedAgentConfig(t *testing.T) {
	db := newTestDB(t)
	seedAgent(t, db, "a1")
	seedConfig(t, db, "cA", "A")
	seedConfig(t, db, "cB", "B")

	_ = db.RecordAgentConfig(models.AgentConfig{AgentID: "a1", ConfigID: "cA", Status: "applied"})
	_ = db.RecordAgentConfig(models.AgentConfig{AgentID: "a1", ConfigID: "cB", Status: "failed"})

	ac, err := db.GetLastAppliedAgentConfig("a1")
	if err != nil {
		t.Fatal(err)
	}
	if ac == nil || ac.ConfigID != "cA" || ac.Content != "A" {
		t.Fatalf("expected cA content A, got %+v", ac)
	}
}

func TestGetLastAppliedAgentConfig_None(t *testing.T) {
	db := newTestDB(t)
	seedAgent(t, db, "a1")
	ac, err := db.GetLastAppliedAgentConfig("a1")
	if err != nil {
		t.Fatal(err)
	}
	if ac != nil {
		t.Fatalf("expected nil, got %+v", ac)
	}
}
