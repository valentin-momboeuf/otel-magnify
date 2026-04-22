package store

import (
	"testing"
	"time"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

func TestInsertAndListWorkloadEvents(t *testing.T) {
	db := newTestDB(t) // existing helper in testhelper_test.go
	// Parent workload must exist because of ON DELETE CASCADE FK.
	if err := db.UpsertWorkload(models.Workload{ID: "w1", Type: "collector", Status: "connected", LastSeenAt: time.Now().UTC()}); err != nil {
		t.Fatalf("seed workload: %v", err)
	}

	t0 := time.Unix(1_700_000_000, 0).UTC()
	for i, ev := range []models.WorkloadEvent{
		{WorkloadID: "w1", InstanceUID: "ia", PodName: "pa", EventType: "connected", Version: "1.0", OccurredAt: t0},
		{WorkloadID: "w1", InstanceUID: "ia", PodName: "pa", EventType: "disconnected", OccurredAt: t0.Add(time.Minute)},
		{WorkloadID: "w1", InstanceUID: "ib", PodName: "pb", EventType: "connected", Version: "1.0", OccurredAt: t0.Add(2 * time.Minute)},
	} {
		if _, err := db.InsertWorkloadEvent(ev); err != nil {
			t.Fatalf("event %d: %v", i, err)
		}
	}

	all, err := db.ListWorkloadEvents("w1", 100, time.Time{})
	if err != nil {
		t.Fatalf("ListWorkloadEvents: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("got %d events, want 3", len(all))
	}
	if all[0].EventType != "connected" || all[0].InstanceUID != "ib" {
		t.Fatalf("wrong ordering (newest first): %+v", all[0])
	}

	since := t0.Add(time.Minute + time.Second)
	recent, err := db.ListWorkloadEvents("w1", 100, since)
	if err != nil {
		t.Fatalf("since query: %v", err)
	}
	if len(recent) != 1 {
		t.Fatalf("since filter: got %d, want 1", len(recent))
	}
}

func TestPurgeOldWorkloadEvents(t *testing.T) {
	db := newTestDB(t)
	if err := db.UpsertWorkload(models.Workload{ID: "w1", Type: "collector", Status: "connected", LastSeenAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	past := time.Now().UTC().Add(-48 * time.Hour)
	now := time.Now().UTC()
	if _, err := db.InsertWorkloadEvent(models.WorkloadEvent{WorkloadID: "w1", InstanceUID: "i", EventType: "connected", OccurredAt: past}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.InsertWorkloadEvent(models.WorkloadEvent{WorkloadID: "w1", InstanceUID: "i", EventType: "connected", OccurredAt: now}); err != nil {
		t.Fatal(err)
	}

	n, err := db.PurgeOldWorkloadEvents(now.Add(-24 * time.Hour))
	if err != nil {
		t.Fatalf("PurgeOldWorkloadEvents: %v", err)
	}
	if n != 1 {
		t.Fatalf("got %d, want 1", n)
	}
	remaining, _ := db.ListWorkloadEvents("w1", 100, time.Time{})
	if len(remaining) != 1 {
		t.Fatalf("remaining: %d", len(remaining))
	}
}

func TestInsertWorkloadEventDefaultsOccurredAt(t *testing.T) {
	db := newTestDB(t)
	if err := db.UpsertWorkload(models.Workload{ID: "w1", Type: "collector", Status: "connected", LastSeenAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	id, err := db.InsertWorkloadEvent(models.WorkloadEvent{WorkloadID: "w1", InstanceUID: "i", EventType: "connected"})
	if err != nil {
		t.Fatal(err)
	}
	if id == 0 {
		t.Fatalf("expected non-zero row id")
	}
	list, _ := db.ListWorkloadEvents("w1", 1, time.Time{})
	if len(list) == 0 || list[0].OccurredAt.IsZero() {
		t.Fatalf("expected OccurredAt populated by default, got %+v", list)
	}
}
