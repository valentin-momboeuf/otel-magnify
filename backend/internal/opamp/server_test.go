package opamp

import (
	"testing"
)

func TestNewOpAMPServer(t *testing.T) {
	srv := New(nil, nil)
	if srv == nil {
		t.Fatal("New returned nil")
	}
}

func TestAgentRegistration(t *testing.T) {
	srv := New(nil, nil)
	if srv == nil {
		t.Fatal("New returned nil")
	}
	if srv.ConnectedAgentCount() != 0 {
		t.Errorf("expected 0 connected agents, got %d", srv.ConnectedAgentCount())
	}
}
