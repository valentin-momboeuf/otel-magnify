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
