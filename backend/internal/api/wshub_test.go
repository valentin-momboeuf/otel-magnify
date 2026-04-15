package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"otel-magnify/pkg/models"
)

func TestHub_BroadcastAgentUpdate(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	defer hub.Stop()

	// Start WS server
	server := httptest.NewServer(http.HandlerFunc(hub.HandleWS))
	defer server.Close()

	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/"
	ws, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer ws.Close()

	// Allow time for registration
	time.Sleep(50 * time.Millisecond)

	agent := models.Agent{
		ID: "a1", DisplayName: "test", Status: "connected",
		Type: "collector", LastSeenAt: time.Now().UTC(),
	}
	hub.BroadcastAgentUpdate(agent)

	ws.SetReadDeadline(time.Now().Add(time.Second))
	_, msg, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}

	var event map[string]any
	if err := json.Unmarshal(msg, &event); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if event["type"] != "agent_update" {
		t.Errorf("type = %q, want agent_update", event["type"])
	}
}

func TestBroadcastConfigStatus_SerializesEvent(t *testing.T) {
	h := NewHub()
	go h.Run()
	defer h.Stop()

	ch := make(chan []byte, 1)
	h.mu.Lock()
	h.clients[&wsClient{send: ch}] = true
	h.mu.Unlock()

	h.BroadcastConfigStatus("agent-1", models.RemoteConfigStatus{
		Status: "failed", ConfigHash: "abc", ErrorMessage: "boom",
		UpdatedAt: time.Unix(0, 0).UTC(),
	})

	select {
	case b := <-ch:
		var ev map[string]any
		_ = json.Unmarshal(b, &ev)
		if ev["type"] != "agent_config_status" || ev["agent_id"] != "agent-1" {
			t.Fatalf("unexpected event: %s", string(b))
		}
		st := ev["status"].(map[string]any)
		if st["status"] != "failed" || st["error_message"] != "boom" {
			t.Fatalf("status payload: %+v", st)
		}
	case <-time.After(time.Second):
		t.Fatal("no broadcast received")
	}
}

func TestBroadcastAutoRollback_SerializesEvent(t *testing.T) {
	h := NewHub()
	go h.Run()
	defer h.Stop()

	ch := make(chan []byte, 1)
	h.mu.Lock()
	h.clients[&wsClient{send: ch}] = true
	h.mu.Unlock()

	h.BroadcastAutoRollback("agent-1", "bbbbbbbb", "aaaaaaaa", "oops")
	select {
	case b := <-ch:
		var ev map[string]any
		_ = json.Unmarshal(b, &ev)
		if ev["type"] != "auto_rollback_applied" || ev["from_hash"] != "bbbbbbbb" || ev["to_hash"] != "aaaaaaaa" {
			t.Fatalf("payload: %s", string(b))
		}
	case <-time.After(time.Second):
		t.Fatal("no broadcast")
	}
}
