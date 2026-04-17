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

	engine := New(db, nil, 5*time.Minute, "")
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

	engine := New(db, nil, 5*time.Minute, "")
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
	engine := New(db, nil, 5*time.Minute, "")
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

func TestEvaluate_ConfigDrift(t *testing.T) {
	db := newTestDB(t)

	// Agent is healthy (seen recently) but has a pending config older than 5 min.
	db.UpsertAgent(models.Agent{
		ID: "a2", Type: "collector", Status: "connected",
		LastSeenAt: time.Now().UTC(), Labels: models.Labels{},
	})

	// Insert a config so we have a valid config_id to reference.
	configID := "cccccccccccccccccccccccccccccccc"
	db.CreateConfig(models.Config{
		ID: configID, Name: "test-cfg", Content: "receivers: []",
		CreatedAt: time.Now().UTC().Add(-10 * time.Minute), CreatedBy: "test",
	})

	// Manually insert an agent_config row with a timestamp older than 5 min.
	_, err := db.Exec(
		`INSERT INTO agent_configs (agent_id, config_id, applied_at, status) VALUES (?, ?, ?, ?)`,
		"a2", configID, time.Now().UTC().Add(-10*time.Minute), "pending",
	)
	if err != nil {
		t.Fatalf("insert agent_config: %v", err)
	}

	engine := New(db, nil, 5*time.Minute, "")
	engine.Evaluate()

	alerts, err := db.ListAlerts(false)
	if err != nil {
		t.Fatal(err)
	}

	var found bool
	for _, a := range alerts {
		if a.AgentID == "a2" && a.Rule == "config_drift" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected config_drift alert for agent a2, got %+v", alerts)
	}
}

func TestEvaluate_ConfigDrift_Resolves(t *testing.T) {
	db := newTestDB(t)

	db.UpsertAgent(models.Agent{
		ID: "a3", Type: "collector", Status: "connected",
		LastSeenAt: time.Now().UTC(), Labels: models.Labels{},
	})

	configID := "dddddddddddddddddddddddddddddddd"
	db.CreateConfig(models.Config{
		ID: configID, Name: "test-cfg2", Content: "receivers: []",
		CreatedAt: time.Now().UTC().Add(-10 * time.Minute), CreatedBy: "test",
	})

	// Start with a stale pending config to fire the alert.
	_, err := db.Exec(
		`INSERT INTO agent_configs (agent_id, config_id, applied_at, status) VALUES (?, ?, ?, ?)`,
		"a3", configID, time.Now().UTC().Add(-10*time.Minute), "pending",
	)
	if err != nil {
		t.Fatalf("insert agent_config: %v", err)
	}

	engine := New(db, nil, 5*time.Minute, "")
	engine.Evaluate()

	// Simulate agent applying the config: update status to "applied".
	_, err = db.Exec(`UPDATE agent_configs SET status = 'applied' WHERE agent_id = ? AND config_id = ?`, "a3", configID)
	if err != nil {
		t.Fatalf("update agent_config: %v", err)
	}

	engine.Evaluate()

	alerts, err := db.ListAlerts(false)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range alerts {
		if a.AgentID == "a3" && a.Rule == "config_drift" {
			t.Errorf("config_drift alert should be resolved but is still unresolved: %+v", a)
		}
	}
}

func TestEvaluate_VersionOutdated(t *testing.T) {
	db := newTestDB(t)

	db.UpsertAgent(models.Agent{
		ID: "a4", Type: "collector", Version: "0.8.0", Status: "connected",
		LastSeenAt: time.Now().UTC(), Labels: models.Labels{},
	})

	engine := New(db, nil, 5*time.Minute, "0.9.0")
	engine.Evaluate()

	alerts, err := db.ListAlerts(false)
	if err != nil {
		t.Fatal(err)
	}

	var found bool
	for _, a := range alerts {
		if a.AgentID == "a4" && a.Rule == "version_outdated" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected version_outdated alert for agent a4, got %+v", alerts)
	}
}

func TestEvaluate_VersionOutdated_Resolves(t *testing.T) {
	db := newTestDB(t)

	db.UpsertAgent(models.Agent{
		ID: "a5", Type: "collector", Version: "0.8.0", Status: "connected",
		LastSeenAt: time.Now().UTC(), Labels: models.Labels{},
	})

	engine := New(db, nil, 5*time.Minute, "0.9.0")
	engine.Evaluate()

	// Agent upgrades its version.
	db.UpsertAgent(models.Agent{
		ID: "a5", Type: "collector", Version: "0.9.0", Status: "connected",
		LastSeenAt: time.Now().UTC(), Labels: models.Labels{},
	})
	engine.Evaluate()

	alerts, err := db.ListAlerts(false)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range alerts {
		if a.AgentID == "a5" && a.Rule == "version_outdated" {
			t.Errorf("version_outdated alert should be resolved but is still unresolved: %+v", a)
		}
	}
}
