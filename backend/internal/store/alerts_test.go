package store

import (
	"testing"
	"time"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

func TestCreateAlert(t *testing.T) {
	db := newTestDB(t)
	db.UpsertAgent(models.Agent{
		ID: "a1", Type: "collector", Status: "connected",
		LastSeenAt: time.Now().UTC(), Labels: models.Labels{},
	})

	alert := models.Alert{
		ID:       "alert-001",
		AgentID:  "a1",
		Rule:     "agent_down",
		Severity: "critical",
		Message:  "Agent a1 not seen for 5 minutes",
		FiredAt:  time.Now().UTC().Truncate(time.Second),
	}

	if err := db.CreateAlert(alert); err != nil {
		t.Fatalf("CreateAlert: %v", err)
	}

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

func TestResolveAlert(t *testing.T) {
	db := newTestDB(t)
	db.UpsertAgent(models.Agent{
		ID: "a1", Type: "collector", Status: "connected",
		LastSeenAt: time.Now().UTC(), Labels: models.Labels{},
	})
	db.CreateAlert(models.Alert{
		ID: "alert-001", AgentID: "a1", Rule: "agent_down",
		Severity: "critical", Message: "down", FiredAt: time.Now().UTC(),
	})

	if err := db.ResolveAlert("alert-001"); err != nil {
		t.Fatalf("ResolveAlert: %v", err)
	}

	alerts, err := db.ListAlerts(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(alerts) != 0 {
		t.Errorf("unresolved count = %d, want 0", len(alerts))
	}

	all, err := db.ListAlerts(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].ResolvedAt == nil {
		t.Error("expected 1 resolved alert")
	}
}

func TestGetUnresolvedAlertByAgentAndRule(t *testing.T) {
	db := newTestDB(t)
	db.UpsertAgent(models.Agent{
		ID: "a1", Type: "collector", Status: "connected",
		LastSeenAt: time.Now().UTC(), Labels: models.Labels{},
	})
	db.CreateAlert(models.Alert{
		ID: "alert-001", AgentID: "a1", Rule: "agent_down",
		Severity: "critical", Message: "down", FiredAt: time.Now().UTC(),
	})

	alert, err := db.GetUnresolvedAlertByAgentAndRule("a1", "agent_down")
	if err != nil {
		t.Fatalf("GetUnresolvedAlertByAgentAndRule: %v", err)
	}
	if alert == nil {
		t.Fatal("expected alert, got nil")
	}
	if alert.ID != "alert-001" {
		t.Errorf("ID = %q, want alert-001", alert.ID)
	}
}
