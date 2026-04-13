package alerts

import (
	"testing"
	"time"

	"otel-magnify/internal/store"
	"otel-magnify/pkg/models"
)

func newTestDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestEvaluate_AgentDown(t *testing.T) {
	db := newTestDB(t)

	// Agent last seen 10 minutes ago
	db.UpsertAgent(models.Agent{
		ID: "a1", Type: "collector", Status: "connected",
		LastSeenAt: time.Now().UTC().Add(-10 * time.Minute), Labels: models.Labels{},
	})

	engine := New(db, nil, 5*time.Minute)
	engine.Evaluate()

	alerts, err := db.ListAlerts(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(alerts) != 1 {
		t.Fatalf("len = %d, want 1", len(alerts))
	}
	if alerts[0].Rule != "agent_down" {
		t.Errorf("Rule = %q, want agent_down", alerts[0].Rule)
	}
}

func TestEvaluate_AgentDown_NoDouble(t *testing.T) {
	db := newTestDB(t)

	db.UpsertAgent(models.Agent{
		ID: "a1", Type: "collector", Status: "connected",
		LastSeenAt: time.Now().UTC().Add(-10 * time.Minute), Labels: models.Labels{},
	})

	engine := New(db, nil, 5*time.Minute)
	engine.Evaluate()
	engine.Evaluate()

	alerts, err := db.ListAlerts(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(alerts) != 1 {
		t.Errorf("len = %d, want 1 (no duplicates)", len(alerts))
	}
}

func TestEvaluate_AgentRecovers(t *testing.T) {
	db := newTestDB(t)

	// Agent was down
	db.UpsertAgent(models.Agent{
		ID: "a1", Type: "collector", Status: "connected",
		LastSeenAt: time.Now().UTC().Add(-10 * time.Minute), Labels: models.Labels{},
	})
	engine := New(db, nil, 5*time.Minute)
	engine.Evaluate()

	// Agent comes back
	db.UpsertAgent(models.Agent{
		ID: "a1", Type: "collector", Status: "connected",
		LastSeenAt: time.Now().UTC(), Labels: models.Labels{},
	})
	engine.Evaluate()

	alerts, err := db.ListAlerts(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(alerts) != 0 {
		t.Errorf("unresolved alerts = %d, want 0", len(alerts))
	}
}
