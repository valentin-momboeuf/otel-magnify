package opamp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/open-telemetry/opamp-go/protobufs"
	opampServer "github.com/open-telemetry/opamp-go/server"
	"github.com/open-telemetry/opamp-go/server/types"

	"otel-magnify/pkg/models"
)

// reportsAvailableComponentsCap is the capability bit set by agents that will
// send AgentToServer.available_components. Defined in OpAMP spec ≥ v0.14.
const reportsAvailableComponentsCap = uint64(protobufs.AgentCapabilities_AgentCapabilities_ReportsAvailableComponents)

// acceptsRemoteConfigCap signals that the agent (or the supervisor fronting it)
// can apply a remote config. For bare collectors with the opamp extension this
// bit is unset — only opamp-supervisor sets it. Exposed in the Agent JSON so
// the UI can gate config editing affordances.
const acceptsRemoteConfigCap = uint64(protobufs.AgentCapabilities_AgentCapabilities_AcceptsRemoteConfig)

// OpAMPStore is the subset of store.DB used by the OpAMP server.
type OpAMPStore interface {
	GetAgent(id string) (models.Agent, error)
	UpsertAgent(a models.Agent) error
	UpdateAgentStatus(id, status string) error
	GetConfig(id string) (models.Config, error)
	CreateConfig(c models.Config) error
	RecordAgentConfig(ac models.AgentConfig) error
	UpdateAgentConfigStatus(agentID, configID, status, errorMessage string) error
	GetLastAppliedAgentConfig(agentID string) (*models.AgentConfig, error)
}

// Notifier is called when an agent's state changes, to notify the frontend WS hub.
type Notifier interface {
	BroadcastAgentUpdate(agent models.Agent)
	BroadcastConfigStatus(agentID string, status models.RemoteConfigStatus)
	BroadcastAutoRollback(agentID, fromHash, toHash, reason string)
}

// Server wraps the opamp-go server and manages agent state.
type Server struct {
	opamp    opampServer.OpAMPServer
	store    OpAMPStore
	notifier Notifier

	mu         sync.RWMutex
	conns      map[string]types.Connection // agentUID hex -> connection
	connToUID  map[types.Connection]string // reverse map for O(1) lookup on close

	// pushFn sends a config YAML to an agent. Defaults to PushConfig; overridable in tests.
	pushFn func(agentID string, yaml []byte) error
}

// New creates a new OpAMP server. Both db and notifier can be nil (useful for testing).
func New(db OpAMPStore, notifier Notifier) *Server {
	s := &Server{
		opamp:     opampServer.New(nil),
		store:     db,
		notifier:  notifier,
		conns:     make(map[string]types.Connection),
		connToUID: make(map[types.Connection]string),
	}
	s.pushFn = func(agentID string, yaml []byte) error {
		return s.PushConfig(context.Background(), agentID, yaml)
	}
	return s
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

	// Track connection in both directions for O(1) lookup on close
	s.mu.Lock()
	s.conns[uid] = conn
	s.connToUID[conn] = uid
	s.mu.Unlock()

	// Seed from the previous DB state so heartbeats (which omit
	// AgentDescription and other identity fields) don't clobber them.
	var agent models.Agent
	if s.store != nil {
		if prev, err := s.store.GetAgent(uid); err == nil {
			agent = prev
		}
	}
	agent.ID = uid
	agent.Status = "connected"
	agent.LastSeenAt = time.Now().UTC()
	if agent.Labels == nil {
		agent.Labels = models.Labels{}
	}

	// Extract agent description. Present on the first message from an agent
	// and whenever the agent itself changes (e.g. version bump).
	if desc := msg.AgentDescription; desc != nil {
		agent.Labels = models.Labels{}
		for _, kv := range desc.IdentifyingAttributes {
			switch kv.Key {
			case "service.name":
				agent.DisplayName = kv.Value.GetStringValue()
			case "service.version":
				agent.Version = kv.Value.GetStringValue()
			}
		}
		// Collectors report service.name as "otelcol", "otelcol-contrib", etc.
		agent.Type = "sdk"
		if isCollectorName(agent.DisplayName) {
			agent.Type = "collector"
		}
		for _, kv := range desc.NonIdentifyingAttributes {
			if sv := kv.Value.GetStringValue(); sv != "" {
				agent.Labels[kv.Key] = sv
			}
		}
		// Capture capability. AgentDescription presence is our marker for a
		// full-status message; heartbeats omit it, so we preserve the previous
		// value (already seeded from the DB at the top of onMessage).
		agent.AcceptsRemoteConfig = msg.Capabilities&acceptsRemoteConfigCap != 0
	}

	// Extract health
	if health := msg.Health; health != nil {
		if !health.Healthy {
			agent.Status = "degraded"
		}
	}

	// Persist the reported effective config (if any), otherwise keep the
	// previously known active_config_id already seeded on `agent`.
	if s.store != nil {
		if cfgID := s.persistEffectiveConfig(uid, agent.DisplayName, msg.EffectiveConfig); cfgID != "" {
			agent.ActiveConfigID = &cfgID
		}
	}

	// Available components only ride on full updates; preserve prior snapshot on heartbeats.
	if ac := flattenAvailableComponents(msg.AvailableComponents); ac != nil {
		agent.AvailableComponents = ac
	}

	// If the agent advertises the capability but hasn't sent the list yet,
	// ask it to send one on the next message by setting the report flag on the reply.
	requestComponents := msg.Capabilities&reportsAvailableComponentsCap != 0 && agent.AvailableComponents == nil

	// Persist to store. Skip if we have no type yet (unknown agent that
	// hasn't sent its AgentDescription) — the CHECK constraint would reject it.
	if s.store != nil && agent.Type != "" {
		if err := s.store.UpsertAgent(agent); err != nil {
			log.Printf("Failed to upsert agent %s: %v", uid, err)
		}
	}

	// Notify frontend
	if s.notifier != nil {
		s.notifier.BroadcastAgentUpdate(agent)
	}

	// Handle RemoteConfigStatus reported by the agent (applied / failed / applying).
	if s.store != nil && msg.RemoteConfigStatus != nil {
		s.handleRemoteConfigStatus(uid, msg.RemoteConfigStatus)
	}

	reply := &protobufs.ServerToAgent{
		InstanceUid: msg.InstanceUid,
	}
	if requestComponents {
		reply.Flags = uint64(protobufs.ServerToAgentFlags_ServerToAgentFlags_ReportAvailableComponents)
	}
	return reply
}

// flattenAvailableComponents converts the OpAMP nested representation
// (category -> ComponentDetails{SubComponentMap: type -> ComponentDetails})
// into a flat map of category -> sorted list of component type names.
// Returns nil if the input is empty (e.g. heartbeat).
func flattenAvailableComponents(ac *protobufs.AvailableComponents) *models.AvailableComponents {
	if ac == nil || len(ac.Components) == 0 {
		return nil
	}
	out := &models.AvailableComponents{
		Components: make(map[string][]string, len(ac.Components)),
		Hash:       hex.EncodeToString(ac.Hash),
	}
	for category, details := range ac.Components {
		if details == nil {
			continue
		}
		names := make([]string, 0, len(details.SubComponentMap))
		for name := range details.SubComponentMap {
			names = append(names, name)
		}
		sort.Strings(names)
		out.Components[category] = names
	}
	return out
}

// broadcastDisconnect emits an agent_update with the agent's full persisted
// state plus Status="disconnected" and LastSeenAt=now. If the store read
// fails, it falls back to the pre-fix minimal broadcast so the UI still sees
// the status change (graceful degradation).
func (s *Server) broadcastDisconnect(uid string) {
	agent, err := s.store.GetAgent(uid)
	if err != nil {
		log.Printf("Failed to reload agent %s for disconnect broadcast: %v", uid, err)
		agent = models.Agent{ID: uid}
	}
	agent.Status = "disconnected"
	agent.LastSeenAt = time.Now().UTC()
	s.notifier.BroadcastAgentUpdate(agent)
}

func (s *Server) onConnectionClose(conn types.Connection) {
	s.mu.Lock()
	defer s.mu.Unlock()

	uid, ok := s.connToUID[conn]
	if !ok {
		// Connection was never registered (e.g. closed before first message)
		return
	}

	delete(s.conns, uid)
	delete(s.connToUID, conn)

	if s.store != nil {
		if err := s.store.UpdateAgentStatus(uid, "disconnected"); err != nil {
			log.Printf("Failed to update agent %s status: %v", uid, err)
		}
		if s.notifier != nil {
			s.broadcastDisconnect(uid)
		}
	}
}

// persistEffectiveConfig stores the YAML config reported by the agent (deduplicated
// by content hash) and returns the resulting config ID. Returns empty if the message
// carries no effective config (typical for heartbeats).
func (s *Server) persistEffectiveConfig(agentUID, displayName string, effective *protobufs.EffectiveConfig) string {
	if effective == nil || effective.ConfigMap == nil || len(effective.ConfigMap.ConfigMap) == 0 {
		return ""
	}

	// Collectors typically report a single file under the empty key "".
	// Concatenate deterministically if multiple files are present.
	keys := make([]string, 0, len(effective.ConfigMap.ConfigMap))
	for k := range effective.ConfigMap.ConfigMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var buf []byte
	for _, k := range keys {
		buf = append(buf, effective.ConfigMap.ConfigMap[k].Body...)
	}
	if len(buf) == 0 {
		return ""
	}

	sum := sha256.Sum256(buf)
	configID := hex.EncodeToString(sum[:])

	if _, err := s.store.GetConfig(configID); err != nil {
		name := fmt.Sprintf("%s-reported-%s", fallback(displayName, agentUID[:8]), configID[:8])
		cfg := models.Config{
			ID:        configID,
			Name:      name,
			Content:   string(buf),
			CreatedAt: time.Now().UTC(),
			CreatedBy: "agent-reported",
		}
		if err := s.store.CreateConfig(cfg); err != nil {
			log.Printf("Failed to persist effective config %s: %v", configID[:8], err)
			return ""
		}
	}

	return configID
}

func fallback(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// isCollectorName returns true if the service.name indicates an OTel Collector.
// Collectors typically report as "otelcol", "otelcol-contrib", "otelcol-custom",
// or "io.opentelemetry.collector".
func isCollectorName(name string) bool {
	n := strings.ToLower(name)
	return strings.HasPrefix(n, "otelcol") ||
		strings.Contains(n, "opentelemetry-collector") ||
		strings.Contains(n, "opentelemetry.collector")
}
