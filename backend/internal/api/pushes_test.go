package api

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

func TestPushActivity_Unauthorized(t *testing.T) {
	_, router, _ := newTestAPI(t)
	req := httptest.NewRequest("GET", "/api/pushes/activity?window=7d", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestPushActivity_EmptyReturnsSevenZeroDays(t *testing.T) {
	_, router, _ := newTestAPI(t)

	req := authedRequest(t, "GET", "/api/pushes/activity?window=7d")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var points []models.PushActivityPoint
	if err := json.NewDecoder(rec.Body).Decode(&points); err != nil {
		t.Fatal(err)
	}
	if len(points) != 7 {
		t.Fatalf("len = %d, want 7", len(points))
	}
	for i, p := range points {
		if p.Count != 0 {
			t.Errorf("points[%d].Count = %d, want 0", i, p.Count)
		}
		if p.Day == "" {
			t.Errorf("points[%d].Day is empty", i)
		}
	}
}

func TestPushActivity_BucketsByDay(t *testing.T) {
	db, router, _ := newTestAPI(t)

	now := time.Now().UTC()
	today := now.Truncate(24 * time.Hour)
	threeDaysAgo := today.AddDate(0, 0, -3)

	// Seed a workload + config so FK constraints pass. The config id is the
	// content hash in production; for tests any non-empty string works.
	if err := db.UpsertWorkload(models.Workload{
		ID: "w1", Type: "collector", Status: "connected",
		LastSeenAt: now, Labels: models.Labels{},
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateConfig(models.Config{
		ID: "cfg-1", Name: "test", Content: "receivers:", CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	// Two pushes today, one three days ago.
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(db.RecordWorkloadConfig(models.WorkloadConfig{
		WorkloadID: "w1", ConfigID: "cfg-1", AppliedAt: today.Add(1 * time.Hour), Status: "applied",
	}))
	must(db.RecordWorkloadConfig(models.WorkloadConfig{
		WorkloadID: "w1", ConfigID: "cfg-1", AppliedAt: today.Add(2 * time.Hour), Status: "applied",
	}))
	must(db.RecordWorkloadConfig(models.WorkloadConfig{
		WorkloadID: "w1", ConfigID: "cfg-1", AppliedAt: threeDaysAgo.Add(5 * time.Hour), Status: "applied",
	}))

	req := authedRequest(t, "GET", "/api/pushes/activity?window=7d")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var points []models.PushActivityPoint
	if err := json.NewDecoder(rec.Body).Decode(&points); err != nil {
		t.Fatal(err)
	}
	if len(points) != 7 {
		t.Fatalf("len = %d, want 7", len(points))
	}

	byDay := make(map[string]int, len(points))
	for _, p := range points {
		byDay[p.Day] = p.Count
	}
	todayKey := today.Format("2006-01-02")
	threeAgoKey := threeDaysAgo.Format("2006-01-02")
	if byDay[todayKey] != 2 {
		t.Errorf("byDay[%s] = %d, want 2", todayKey, byDay[todayKey])
	}
	if byDay[threeAgoKey] != 1 {
		t.Errorf("byDay[%s] = %d, want 1", threeAgoKey, byDay[threeAgoKey])
	}
}

func TestPushActivity_RejectsUnsupportedWindow(t *testing.T) {
	_, router, _ := newTestAPI(t)
	req := authedRequest(t, "GET", "/api/pushes/activity?window=30d")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != 400 {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
