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

func TestOnMessage_AcceptsRemoteConfigCapabilityPersisted(t *testing.T) {
	s, db, _ := newTestServer(t)

	uid := make([]byte, 16)
	uid[0] = 0xCC
	uidHex := hex.EncodeToString(uid)

	// Full-status message with AcceptsRemoteConfig set.
	full := &protobufs.AgentToServer{
		InstanceUid: uid,
		AgentDescription: &protobufs.AgentDescription{
			IdentifyingAttributes: []*protobufs.KeyValue{
				{Key: "service.name", Value: &protobufs.AnyValue{Value: &protobufs.AnyValue_StringValue{StringValue: "otelcol-contrib"}}},
				{Key: "service.version", Value: &protobufs.AnyValue{Value: &protobufs.AnyValue_StringValue{StringValue: "0.150.1"}}},
			},
		},
		Capabilities: uint64(protobufs.AgentCapabilities_AgentCapabilities_AcceptsRemoteConfig),
	}
	s.onMessage(nil, nil, full)

	got, err := db.GetAgent(uidHex)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !got.AcceptsRemoteConfig {
		t.Fatalf("after full-status: accepts_remote_config=false, want true")
	}

	// Heartbeat (no AgentDescription, Capabilities=0) must preserve the previous value.
	hb := &protobufs.AgentToServer{InstanceUid: uid}
	s.onMessage(nil, nil, hb)
	got, _ = db.GetAgent(uidHex)
	if !got.AcceptsRemoteConfig {
		t.Fatalf("after heartbeat: accepts_remote_config flipped to false — should be preserved")
	}

	// A full-status without the bit flips it off.
	fullOff := &protobufs.AgentToServer{
		InstanceUid:      uid,
		AgentDescription: full.AgentDescription,
		Capabilities:     0,
	}
	s.onMessage(nil, nil, fullOff)
	got, _ = db.GetAgent(uidHex)
	if got.AcceptsRemoteConfig {
		t.Fatalf("after full-status with caps=0: accepts_remote_config stayed true")
	}
}

func TestBroadcastDisconnect_HydratesAgent(t *testing.T) {
	s, db, n := newTestServer(t)

	uid := make([]byte, 16)
	uid[0] = 0xDD
	uidHex := hex.EncodeToString(uid)

	cfgID := "deadbeef"
	// agents.active_config_id has a FK to configs(id); seed the config first.
	if err := db.CreateConfig(models.Config{
		ID:        cfgID,
		Name:      "cfg",
		Content:   "x",
		CreatedAt: time.Now().UTC(),
		CreatedBy: "u",
	}); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	seeded := models.Agent{
		ID:                  uidHex,
		DisplayName:         "prod-eu",
		Type:                "collector",
		Version:             "0.150.1",
		Status:              "connected",
		LastSeenAt:          time.Now().UTC().Add(-time.Minute),
		Labels:              models.Labels{"env": "prod"},
		ActiveConfigID:      &cfgID,
		AcceptsRemoteConfig: true,
	}
	if err := db.UpsertAgent(seeded); err != nil {
		t.Fatalf("seed agent: %v", err)
	}

	before := time.Now().UTC()
	s.broadcastDisconnect(uidHex)
	after := time.Now().UTC()

	if len(n.agents) != 1 {
		t.Fatalf("expected 1 broadcast, got %d", len(n.agents))
	}
	got := n.agents[0]

	if got.ID != uidHex {
		t.Errorf("ID: got %q, want %q", got.ID, uidHex)
	}
	if got.Status != "disconnected" {
		t.Errorf("Status: got %q, want %q", got.Status, "disconnected")
	}
	if got.LastSeenAt.Before(before) || got.LastSeenAt.After(after) {
		t.Errorf("LastSeenAt: got %v, want within [%v,%v]", got.LastSeenAt, before, after)
	}
	if got.DisplayName != "prod-eu" {
		t.Errorf("DisplayName lost: got %q", got.DisplayName)
	}
	if got.Type != "collector" {
		t.Errorf("Type lost: got %q", got.Type)
	}
	if got.Version != "0.150.1" {
		t.Errorf("Version lost: got %q", got.Version)
	}
	if got.Labels["env"] != "prod" {
		t.Errorf("Labels lost: got %#v", got.Labels)
	}
	if got.ActiveConfigID == nil || *got.ActiveConfigID != cfgID {
		t.Errorf("ActiveConfigID lost: got %v", got.ActiveConfigID)
	}
	if !got.AcceptsRemoteConfig {
		t.Errorf("AcceptsRemoteConfig lost: got false")
	}
}

func TestBroadcastDisconnect_FallbackOnDBError(t *testing.T) {
	s, db, n := newTestServer(t)

	uid := make([]byte, 16)
	uid[0] = 0xEE
	uidHex := hex.EncodeToString(uid)
	if err := db.UpsertAgent(models.Agent{
		ID:         uidHex,
		Type:       "collector",
		Status:     "connected",
		LastSeenAt: time.Now().UTC(),
		Labels:     models.Labels{},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Force GetAgent to fail by closing the underlying DB before the call.
	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	before := time.Now().UTC()
	s.broadcastDisconnect(uidHex) // must not panic
	after := time.Now().UTC()

	if len(n.agents) != 1 {
		t.Fatalf("expected 1 broadcast, got %d", len(n.agents))
	}
	got := n.agents[0]
	if got.ID != uidHex {
		t.Errorf("ID: got %q, want %q", got.ID, uidHex)
	}
	if got.Status != "disconnected" {
		t.Errorf("Status: got %q, want %q", got.Status, "disconnected")
	}
	if got.LastSeenAt.Before(before) || got.LastSeenAt.After(after) {
		t.Errorf("LastSeenAt: got %v, want within [%v,%v]", got.LastSeenAt, before, after)
	}
}
