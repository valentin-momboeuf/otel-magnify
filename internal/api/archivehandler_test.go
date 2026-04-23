package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

func TestHandleArchiveWorkload_EditorCanArchive(t *testing.T) {
	db, auth := newMeTestAPI(t)
	if err := db.UpsertWorkload(models.Workload{
		ID: "w1", Type: "collector", Status: "connected",
		LastSeenAt:      time.Now().UTC(),
		Labels:          models.Labels{},
		FingerprintKeys: models.FingerprintKeys{},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	tok, _ := auth.GenerateToken("u1", "u1@x.com", []string{"editor"})
	req := httptest.NewRequest(http.MethodPost, "/api/workloads/w1/archive", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	buildMeTestRouter(db, auth).ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleArchiveWorkload_ViewerForbidden(t *testing.T) {
	db, auth := newMeTestAPI(t)
	tok, _ := auth.GenerateToken("u1", "u1@x.com", []string{"viewer"})
	req := httptest.NewRequest(http.MethodPost, "/api/workloads/w1/archive", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	buildMeTestRouter(db, auth).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d, want 403", rec.Code)
	}
}
