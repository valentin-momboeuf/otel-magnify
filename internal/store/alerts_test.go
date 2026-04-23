package store

import (
	"testing"
	"time"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

func TestCreateAlert(t *testing.T) {
	db := newTestDB(t)
	seedWorkload(t, db, "a1")

	alert := models.Alert{
		ID:         "alert-001",
		WorkloadID: "a1",
		Rule:       "workload_down",
		Severity:   "critical",
		Message:    "Workload a1 not seen for 5 minutes",
		FiredAt:    time.Now().UTC().Truncate(time.Second),
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
	if alerts[0].Rule != "workload_down" {
		t.Errorf("Rule = %q, want workload_down", alerts[0].Rule)
	}
	if alerts[0].WorkloadID != "a1" {
		t.Errorf("WorkloadID = %q, want a1", alerts[0].WorkloadID)
	}
}

func TestResolveAlert(t *testing.T) {
	db := newTestDB(t)
	seedWorkload(t, db, "a1")
	if err := db.CreateAlert(models.Alert{
		ID: "alert-001", WorkloadID: "a1", Rule: "workload_down",
		Severity: "critical", Message: "down", FiredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

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

func TestGetUnresolvedAlertByWorkloadAndRule(t *testing.T) {
	db := newTestDB(t)
	seedWorkload(t, db, "a1")
	if err := db.CreateAlert(models.Alert{
		ID: "alert-001", WorkloadID: "a1", Rule: "workload_down",
		Severity: "critical", Message: "down", FiredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	alert, err := db.GetUnresolvedAlertByWorkloadAndRule("a1", "workload_down")
	if err != nil {
		t.Fatalf("GetUnresolvedAlertByWorkloadAndRule: %v", err)
	}
	if alert == nil {
		t.Fatal("expected alert, got nil")
	}
	if alert.ID != "alert-001" {
		t.Errorf("ID = %q, want alert-001", alert.ID)
	}
	if alert.WorkloadID != "a1" {
		t.Errorf("WorkloadID = %q, want a1", alert.WorkloadID)
	}
}
