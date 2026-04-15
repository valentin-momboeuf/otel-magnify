package opamp

import (
	"encoding/hex"
	"testing"
	"time"

	"github.com/open-telemetry/opamp-go/protobufs"

	"otel-magnify/internal/store"
	"otel-magnify/pkg/models"
)

type fakeNotifier struct {
	agents   []models.Agent
	statuses []struct {
		agentID string
		status  models.RemoteConfigStatus
	}
	rollbacks []struct {
		agentID, fromHash, toHash, reason string
	}
}

func (f *fakeNotifier) BroadcastAgentUpdate(a models.Agent) { f.agents = append(f.agents, a) }
func (f *fakeNotifier) BroadcastConfigStatus(agentID string, s models.RemoteConfigStatus) {
	f.statuses = append(f.statuses, struct {
		agentID string
		status  models.RemoteConfigStatus
	}{agentID, s})
}
func (f *fakeNotifier) BroadcastAutoRollback(agentID, fromHash, toHash, reason string) {
	f.rollbacks = append(f.rollbacks, struct {
		agentID, fromHash, toHash, reason string
	}{agentID, fromHash, toHash, reason})
}

func newTestServer(t *testing.T) (*Server, *store.DB, *fakeNotifier) {
	t.Helper()
	db, err := store.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	n := &fakeNotifier{}
	return New(db, n), db, n
}

func TestOnMessage_RemoteConfigStatusApplied(t *testing.T) {
	s, db, n := newTestServer(t)

	uid := make([]byte, 16)
	uid[0] = 0xAA
	uidHex := hex.EncodeToString(uid)
	_ = db.UpsertAgent(models.Agent{ID: uidHex, Type: "collector", Status: "connected", LastSeenAt: time.Now().UTC(), Labels: models.Labels{}})
	_ = db.CreateConfig(models.Config{ID: "deadbeef", Name: "n", Content: "x", CreatedAt: time.Now().UTC(), CreatedBy: "u"})
	_ = db.RecordAgentConfig(models.AgentConfig{AgentID: uidHex, ConfigID: "deadbeef", Status: "pending"})

	hashBytes, _ := hex.DecodeString("deadbeef")
	msg := &protobufs.AgentToServer{
		InstanceUid: uid,
		RemoteConfigStatus: &protobufs.RemoteConfigStatus{
			LastRemoteConfigHash: hashBytes,
			Status:               protobufs.RemoteConfigStatuses_RemoteConfigStatuses_APPLIED,
		},
	}
	s.onMessage(nil, nil, msg)

	hist, _ := db.GetAgentConfigHistory(uidHex)
	if len(hist) != 1 || hist[0].Status != "applied" {
		t.Fatalf("expected applied row, got %+v", hist)
	}
	if len(n.statuses) == 0 || n.statuses[0].status.Status != "applied" {
		t.Fatalf("expected applied status broadcast, got %+v", n.statuses)
	}
}

func TestOnMessage_RemoteConfigStatusFailed_AutoRollback(t *testing.T) {
	s, db, n := newTestServer(t)

	uid := make([]byte, 16)
	uid[0] = 0xBB
	uidHex := hex.EncodeToString(uid)
	_ = db.UpsertAgent(models.Agent{ID: uidHex, Type: "collector", Status: "connected", LastSeenAt: time.Now().UTC(), Labels: models.Labels{}})

	_ = db.CreateConfig(models.Config{ID: "aaaaaaaa", Name: "A", Content: "good-yaml", CreatedAt: time.Now().UTC().Add(-time.Hour), CreatedBy: "u"})
	_ = db.RecordAgentConfig(models.AgentConfig{AgentID: uidHex, ConfigID: "aaaaaaaa", Status: "applied", AppliedAt: time.Now().UTC().Add(-time.Hour)})
	_ = db.CreateConfig(models.Config{ID: "bbbbbbbb", Name: "B", Content: "bad-yaml", CreatedAt: time.Now().UTC(), CreatedBy: "u"})
	_ = db.RecordAgentConfig(models.AgentConfig{AgentID: uidHex, ConfigID: "bbbbbbbb", Status: "pending"})

	pushes := [][]byte{}
	s.pushFn = func(agentID string, yaml []byte) error {
		pushes = append(pushes, yaml)
		return nil
	}

	hashB, _ := hex.DecodeString("bbbbbbbb")
	msg := &protobufs.AgentToServer{
		InstanceUid: uid,
		RemoteConfigStatus: &protobufs.RemoteConfigStatus{
			LastRemoteConfigHash: hashB,
			Status:               protobufs.RemoteConfigStatuses_RemoteConfigStatuses_FAILED,
			ErrorMessage:         "unknown exporter 'othttp'",
		},
	}
	s.onMessage(nil, nil, msg)

	hist, _ := db.GetAgentConfigHistory(uidHex)
	var bRow *models.AgentConfig
	for i := range hist {
		if hist[i].ConfigID == "bbbbbbbb" {
			bRow = &hist[i]
		}
	}
	if bRow == nil || bRow.Status != "failed" || bRow.ErrorMessage != "unknown exporter 'othttp'" {
		t.Fatalf("B row not updated to failed: %+v", bRow)
	}
	if len(pushes) != 1 || string(pushes[0]) != "good-yaml" {
		t.Fatalf("expected auto-rollback to re-push A, pushes=%v", pushes)
	}
	if len(n.rollbacks) != 1 || n.rollbacks[0].toHash != "aaaaaaaa" {
		t.Fatalf("expected rollback notification, got %+v", n.rollbacks)
	}
}

func TestOnMessage_RemoteConfigStatusFailed_NoRollbackTarget(t *testing.T) {
	s, db, n := newTestServer(t)

	uid := make([]byte, 16)
	uid[0] = 0xCC
	uidHex := hex.EncodeToString(uid)
	_ = db.UpsertAgent(models.Agent{ID: uidHex, Type: "collector", Status: "connected", LastSeenAt: time.Now().UTC(), Labels: models.Labels{}})
	_ = db.CreateConfig(models.Config{ID: "cccccccc", Name: "C", Content: "bad", CreatedAt: time.Now().UTC(), CreatedBy: "u"})
	_ = db.RecordAgentConfig(models.AgentConfig{AgentID: uidHex, ConfigID: "cccccccc", Status: "pending"})

	pushes := [][]byte{}
	s.pushFn = func(_ string, y []byte) error { pushes = append(pushes, y); return nil }

	hash, _ := hex.DecodeString("cccccccc")
	s.onMessage(nil, nil, &protobufs.AgentToServer{
		InstanceUid: uid,
		RemoteConfigStatus: &protobufs.RemoteConfigStatus{
			LastRemoteConfigHash: hash,
			Status:               protobufs.RemoteConfigStatuses_RemoteConfigStatuses_FAILED,
			ErrorMessage:         "boom",
		},
	})

	if len(pushes) != 0 {
		t.Fatalf("expected no rollback push, got %d", len(pushes))
	}
	if len(n.rollbacks) != 0 {
		t.Fatalf("expected no rollback notification")
	}
}
