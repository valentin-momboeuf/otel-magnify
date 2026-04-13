package opamp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/open-telemetry/opamp-go/protobufs"
	opampServer "github.com/open-telemetry/opamp-go/server"
	"github.com/open-telemetry/opamp-go/server/types"

	"otel-magnify/internal/store"
	"otel-magnify/pkg/models"
)

// Notifier is called when an agent's state changes, to notify the frontend WS hub.
type Notifier interface {
	BroadcastAgentUpdate(agent models.Agent)
}

// Server wraps the opamp-go server and manages agent state.
type Server struct {
	opamp    opampServer.OpAMPServer
	store    *store.DB
	notifier Notifier

	mu    sync.RWMutex
	conns map[string]types.Connection // agentUID hex -> connection
}

// New creates a new OpAMP server. Both db and notifier can be nil (useful for testing).
func New(db *store.DB, notifier Notifier) *Server {
	return &Server{
		opamp:    opampServer.New(nil),
		store:    db,
		notifier: notifier,
		conns:    make(map[string]types.Connection),
	}
}

// ConnectedAgentCount returns the number of currently connected agents.
func (s *Server) ConnectedAgentCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.conns)
}

// GetConnection returns the OpAMP connection for a given agent ID, or nil.
func (s *Server) GetConnection(agentID string) types.Connection {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.conns[agentID]
}

// PushConfig sends a remote config to a connected agent.
func (s *Server) PushConfig(ctx context.Context, agentID string, yamlContent []byte) error {
	conn := s.GetConnection(agentID)
	if conn == nil {
		return fmt.Errorf("agent %s not connected", agentID)
	}

	configHash := sha256.Sum256(yamlContent)

	msg := &protobufs.ServerToAgent{
		InstanceUid: []byte(agentID),
		RemoteConfig: &protobufs.AgentRemoteConfig{
			Config: &protobufs.AgentConfigMap{
				ConfigMap: map[string]*protobufs.AgentConfigFile{
					"": {
						Body:        yamlContent,
						ContentType: "text/yaml",
					},
				},
			},
			ConfigHash: configHash[:],
		},
	}
	return conn.Send(ctx, msg)
}

// Attach mounts the OpAMP handler on an existing HTTP mux.
// Returns the HTTPHandlerFunc and ConnContext to register on the HTTP server.
func (s *Server) Attach() (opampServer.HTTPHandlerFunc, opampServer.ConnContext, error) {
	connCallbacks := types.ConnectionCallbacks{
		OnConnected:       s.onConnected,
		OnMessage:         s.onMessage,
		OnConnectionClose: s.onConnectionClose,
	}

	settings := opampServer.Settings{
		Callbacks: types.Callbacks{
			OnConnecting: func(request *http.Request) types.ConnectionResponse {
				return types.ConnectionResponse{
					Accept:              true,
					ConnectionCallbacks: connCallbacks,
				}
			},
		},
	}

	return s.opamp.Attach(settings)
}

// Stop gracefully shuts down the OpAMP server.
func (s *Server) Stop(ctx context.Context) error {
	return s.opamp.Stop(ctx)
}

func (s *Server) onConnected(ctx context.Context, conn types.Connection) {
	log.Printf("OpAMP agent connected: %v", conn)
}

func (s *Server) onMessage(ctx context.Context, conn types.Connection, msg *protobufs.AgentToServer) *protobufs.ServerToAgent {
	uid := hex.EncodeToString(msg.InstanceUid)

	// Track connection
	s.mu.Lock()
	s.conns[uid] = conn
	s.mu.Unlock()

	agent := models.Agent{
		ID:         uid,
		Status:     "connected",
		LastSeenAt: time.Now().UTC(),
		Labels:     models.Labels{},
	}

	// Extract agent description
	if desc := msg.AgentDescription; desc != nil {
		for _, kv := range desc.IdentifyingAttributes {
			switch kv.Key {
			case "service.name":
				agent.DisplayName = kv.Value.GetStringValue()
			case "service.version":
				agent.Version = kv.Value.GetStringValue()
			}
		}
		// Determine type from service.name
		agent.Type = "collector"
		if agent.DisplayName != "" && agent.DisplayName != "io.opentelemetry.collector" {
			agent.Type = "sdk"
		}
		// Non-identifying attributes -> labels
		for _, kv := range desc.NonIdentifyingAttributes {
			if sv := kv.Value.GetStringValue(); sv != "" {
				agent.Labels[kv.Key] = sv
			}
		}
	}

	// Extract health
	if health := msg.Health; health != nil {
		if !health.Healthy {
			agent.Status = "degraded"
		}
	}

	// Persist to store
	if s.store != nil {
		if err := s.store.UpsertAgent(agent); err != nil {
			log.Printf("Failed to upsert agent %s: %v", uid, err)
		}
	}

	// Notify frontend
	if s.notifier != nil {
		s.notifier.BroadcastAgentUpdate(agent)
	}

	return &protobufs.ServerToAgent{
		InstanceUid: msg.InstanceUid,
	}
}

func (s *Server) onConnectionClose(conn types.Connection) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for uid, c := range s.conns {
		if c == conn {
			delete(s.conns, uid)
			if s.store != nil {
				if err := s.store.UpdateAgentStatus(uid, "disconnected"); err != nil {
					log.Printf("Failed to update agent %s status: %v", uid, err)
				}
				if s.notifier != nil {
					s.notifier.BroadcastAgentUpdate(models.Agent{
						ID:         uid,
						Status:     "disconnected",
						LastSeenAt: time.Now().UTC(),
					})
				}
			}
			break
		}
	}
}
