package store

import (
	"testing"
	"time"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

func TestUpsertAgent(t *testing.T) {
	db := newTestDB(t)

	agent := models.Agent{
		ID:          "agent-001",
		DisplayName: "collector-eu",
		Type:        "collector",
		Version:     "0.96.0",
		Status:      "connected",
		LastSeenAt:  time.Now().UTC().Truncate(time.Second),
		Labels:      models.Labels{"env": "prod"},
	}

	if err := db.UpsertAgent(agent); err != nil {
		t.Fatalf("UpsertAgent: %v", err)
	}

	got, err := db.GetAgent("agent-001")
	if err != nil {
		t.Fatalf("GetAgent: %v", err)
	}
	if got.DisplayName != "collector-eu" {
		t.Errorf("DisplayName = %q, want %q", got.DisplayName, "collector-eu")
	}
	if got.Labels["env"] != "prod" {
		t.Errorf("Labels[env] = %q, want %q", got.Labels["env"], "prod")
	}
}

func TestUpsertAgent_Update(t *testing.T) {
	db := newTestDB(t)

	agent := models.Agent{
		ID: "agent-001", DisplayName: "v1", Type: "collector",
		Status: "connected", LastSeenAt: time.Now().UTC(), Labels: models.Labels{},
	}
	if err := db.UpsertAgent(agent); err != nil {
		t.Fatalf("UpsertAgent: %v", err)
	}

	agent.DisplayName = "v2"
	agent.Status = "degraded"
	if err := db.UpsertAgent(agent); err != nil {
		t.Fatalf("UpsertAgent update: %v", err)
	}

	got, err := db.GetAgent("agent-001")
	if err != nil {
		t.Fatalf("GetAgent: %v", err)
	}
	if got.DisplayName != "v2" || got.Status != "degraded" {
		t.Errorf("got name=%q status=%q, want v2/degraded", got.DisplayName, got.Status)
	}
}

func TestListAgents(t *testing.T) {
	db := newTestDB(t)

	for _, id := range []string{"a1", "a2", "a3"} {
		err := db.UpsertAgent(models.Agent{
			ID: id, Type: "sdk", Status: "connected",
			LastSeenAt: time.Now().UTC(), Labels: models.Labels{},
		})
		if err != nil {
			t.Fatalf("UpsertAgent %s: %v", id, err)
		}
	}

	agents, err := db.ListAgents()
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if len(agents) != 3 {
		t.Errorf("len = %d, want 3", len(agents))
	}
}

func TestUpdateAgentStatus(t *testing.T) {
	db := newTestDB(t)
	err := db.UpsertAgent(models.Agent{
		ID: "a1", Type: "collector", Status: "connected",
		LastSeenAt: time.Now().UTC(), Labels: models.Labels{},
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := db.UpdateAgentStatus("a1", "disconnected"); err != nil {
		t.Fatalf("UpdateAgentStatus: %v", err)
	}

	got, _ := db.GetAgent("a1")
	if got.Status != "disconnected" {
		t.Errorf("Status = %q, want disconnected", got.Status)
	}
}

func TestUpsertAgent_RoundtripsRemoteConfigStatus(t *testing.T) {
	db := newTestDB(t)
	agent := models.Agent{
		ID: "a1", Type: "collector", Status: "connected",
		LastSeenAt: time.Now().UTC(), Labels: models.Labels{},
		RemoteConfigStatus: &models.RemoteConfigStatus{
			Status:     "applied",
			ConfigHash: "abc",
			UpdatedAt:  time.Now().UTC().Truncate(time.Second),
		},
	}
	if err := db.UpsertAgent(agent); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetAgent("a1")
	if err != nil {
		t.Fatal(err)
	}
	if got.RemoteConfigStatus == nil || got.RemoteConfigStatus.ConfigHash != "abc" {
		t.Fatalf("remote_config_status not persisted: %+v", got.RemoteConfigStatus)
	}
}

func TestAgent_AcceptsRemoteConfig_RoundTrip(t *testing.T) {
	db := newTestDB(t)

	want := models.Agent{
		ID: "a-supervised", DisplayName: "c1", Type: "collector",
		Version: "0.98.0", Status: "connected",
		LastSeenAt: time.Now().UTC(), Labels: models.Labels{},
		AcceptsRemoteConfig: true,
	}
	if err := db.UpsertAgent(want); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, err := db.GetAgent("a-supervised")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !got.AcceptsRemoteConfig {
		t.Fatalf("accepts_remote_config: got false, want true")
	}

	// Flip back to false, confirm round-trip works both ways.
	want.AcceptsRemoteConfig = false
	if err := db.UpsertAgent(want); err != nil {
		t.Fatalf("upsert false: %v", err)
	}
	got, _ = db.GetAgent("a-supervised")
	if got.AcceptsRemoteConfig {
		t.Fatalf("accepts_remote_config: got true, want false")
	}

	// ListAgents must also carry the flag.
	list, _ := db.ListAgents()
	if len(list) != 1 || list[0].AcceptsRemoteConfig {
		t.Fatalf("ListAgents carried wrong value: %+v", list)
	}
}
