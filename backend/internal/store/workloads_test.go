package store

import (
	"testing"
	"time"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

func TestUpsertAndGetWorkload(t *testing.T) {
	db := newTestDB(t)
	w := models.Workload{
		ID:                "wl1",
		FingerprintSource: "k8s",
		FingerprintKeys:   models.FingerprintKeys{"cluster": "prod", "namespace": "obs", "kind": "deployment", "name": "otel"},
		DisplayName:       "otel-collector",
		Type:              "collector",
		Version:           "0.100.0",
		Status:            "connected",
		LastSeenAt:        time.Now().UTC(),
		Labels:            models.Labels{"k8s.pod.name": "otel-abc"},
	}
	if err := db.UpsertWorkload(w); err != nil {
		t.Fatalf("UpsertWorkload: %v", err)
	}

	got, err := db.GetWorkload("wl1")
	if err != nil {
		t.Fatalf("GetWorkload: %v", err)
	}
	if got.FingerprintSource != "k8s" || got.FingerprintKeys["namespace"] != "obs" {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestListWorkloadsExcludesArchived(t *testing.T) {
	db := newTestDB(t)
	now := time.Now().UTC()
	if err := db.UpsertWorkload(models.Workload{ID: "live", Type: "sdk", Status: "connected", LastSeenAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertWorkload(models.Workload{ID: "gone", Type: "sdk", Status: "disconnected", LastSeenAt: now, ArchivedAt: &now}); err != nil {
		t.Fatal(err)
	}

	list, err := db.ListWorkloads(false)
	if err != nil {
		t.Fatalf("ListWorkloads: %v", err)
	}
	if len(list) != 1 || list[0].ID != "live" {
		t.Fatalf("expected only live, got %+v", list)
	}
	allIncl, _ := db.ListWorkloads(true)
	if len(allIncl) != 2 {
		t.Fatalf("expected 2 with includeArchived=true, got %d", len(allIncl))
	}
}

func TestMarkWorkloadDisconnectedSetsRetention(t *testing.T) {
	db := newTestDB(t)
	now := time.Now().UTC()
	if err := db.UpsertWorkload(models.Workload{ID: "w1", Type: "collector", Status: "connected", LastSeenAt: now}); err != nil {
		t.Fatal(err)
	}

	until := now.Add(24 * time.Hour)
	if err := db.MarkWorkloadDisconnected("w1", until); err != nil {
		t.Fatalf("MarkWorkloadDisconnected: %v", err)
	}
	w, err := db.GetWorkload("w1")
	if err != nil {
		t.Fatal(err)
	}
	if w.Status != "disconnected" {
		t.Fatalf("status = %q, want disconnected", w.Status)
	}
	if w.RetentionUntil == nil || !w.RetentionUntil.Equal(until) {
		t.Fatalf("retention_until = %v, want %v", w.RetentionUntil, until)
	}
}

func TestClearWorkloadRetention(t *testing.T) {
	db := newTestDB(t)
	now := time.Now().UTC()
	until := now.Add(time.Hour)
	if err := db.UpsertWorkload(models.Workload{ID: "w1", Type: "collector", Status: "disconnected", LastSeenAt: now, RetentionUntil: &until}); err != nil {
		t.Fatal(err)
	}
	if err := db.ClearWorkloadRetention("w1"); err != nil {
		t.Fatal(err)
	}
	w, _ := db.GetWorkload("w1")
	if w.RetentionUntil != nil {
		t.Fatalf("expected retention_until nil, got %v", w.RetentionUntil)
	}
}

func TestArchiveExpiredWorkloads(t *testing.T) {
	db := newTestDB(t)
	past := time.Now().UTC().Add(-time.Hour)
	future := time.Now().UTC().Add(time.Hour)
	if err := db.UpsertWorkload(models.Workload{ID: "old", Type: "collector", Status: "disconnected", LastSeenAt: past, RetentionUntil: &past}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertWorkload(models.Workload{ID: "young", Type: "collector", Status: "disconnected", LastSeenAt: past, RetentionUntil: &future}); err != nil {
		t.Fatal(err)
	}

	n, err := db.ArchiveExpiredWorkloads(time.Now().UTC())
	if err != nil {
		t.Fatalf("ArchiveExpiredWorkloads: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 archived, got %d", n)
	}
	old, _ := db.GetWorkload("old")
	if old.ArchivedAt == nil {
		t.Fatalf("expected ArchivedAt set")
	}
	young, _ := db.GetWorkload("young")
	if young.ArchivedAt != nil {
		t.Fatalf("expected young not archived")
	}
}

func TestDeleteWorkload(t *testing.T) {
	db := newTestDB(t)
	now := time.Now().UTC()
	if err := db.UpsertWorkload(models.Workload{ID: "w1", Type: "sdk", Status: "connected", LastSeenAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteWorkload("w1"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetWorkload("w1"); err == nil {
		t.Fatalf("expected not-found error after delete")
	}
}
