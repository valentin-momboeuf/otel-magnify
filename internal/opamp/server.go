package opamp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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

	"github.com/magnify-labs/otel-magnify/internal/opampauth"
	"github.com/magnify-labs/otel-magnify/pkg/ext"
	"github.com/magnify-labs/otel-magnify/pkg/models"
)

// reportsAvailableComponentsCap is the capability bit set by agents that will
// send AgentToServer.available_components. Defined in OpAMP spec >= v0.14.
const reportsAvailableComponentsCap = uint64(protobufs.AgentCapabilities_AgentCapabilities_ReportsAvailableComponents)

// acceptsRemoteConfigCap signals that the agent (or the supervisor fronting it)
// can apply a remote config. For bare collectors with the opamp extension this
// bit is unset — only opamp-supervisor sets it. Exposed in the Workload JSON
// so the UI can gate config editing affordances.
const acceptsRemoteConfigCap = uint64(protobufs.AgentCapabilities_AgentCapabilities_AcceptsRemoteConfig)

const defaultWriteTimeout = 5 * time.Second

// Store is the narrow subset of store.DB the OpAMP server needs.
type Store interface {
	GetWorkload(id string) (models.Workload, error)
	UpsertWorkload(w models.Workload) error
	MarkWorkloadDisconnected(id string, retentionUntil time.Time) error
	ClearWorkloadRetention(id string) error

	GetConfig(id string) (models.Config, error)
	CreateConfig(c models.Config) error

	RecordWorkloadConfig(wc models.WorkloadConfig) error
	UpdateWorkloadConfigStatus(workloadID, configID, status, errorMessage string) error
	MarkWorkloadConfigSent(workloadID, configID string, sentAt time.Time) error
	UpdateWorkloadConfigInstanceStatus(workloadID, configID, instanceUID, status, errorMessage string, updatedAt time.Time) error
	GetLatestWorkloadConfig(workloadID string) (*models.WorkloadConfig, error)
	GetLastAppliedWorkloadConfig(workloadID string) (*models.WorkloadConfig, error)
	GetRollbackTarget(workloadID, excludeHash string) (*models.RollbackTarget, error)

	InsertWorkloadEvent(e models.WorkloadEvent) (int64, error)

	ValidateOpAMPToken(ctx context.Context, id string, presentedHash [32]byte, now time.Time) (models.OpAMPTokenPrincipal, error)
	MarkOpAMPTokenUsed(ctx context.Context, id string, now time.Time) error
}

// Notifier is called when a workload's state changes, to relay updates to the
// frontend WS hub.
type Notifier interface {
	BroadcastWorkloadUpdate(workload models.Workload, connectedInstances, driftedInstances int)
	BroadcastWorkloadEvent(event models.WorkloadEvent)
	BroadcastConfigStatus(workloadID string, status models.RemoteConfigStatus)
	BroadcastAutoRollback(workloadID, fromHash, toHash, reason, targetKind string)
}

// Options controls time-based server behavior. Zero values fall back to
// production defaults.
type Options struct {
	// DisconnectGrace is how long to wait after the last live instance of a
	// workload goes away before marking the workload as disconnected. This
	// smooths over K8s rolling restarts where pod A closes its connection
	// moments before pod B opens one.
	DisconnectGrace time.Duration
	// RetentionDuration is how long a disconnected workload stays around
	// before it becomes eligible for archival.
	RetentionDuration time.Duration

	// Tests inject these hooks to exercise exact expiry and timer races.
	now       func() time.Time
	afterFunc connectionAfterFunc
	// writeTimeout bounds socket writes. Tests shorten it to exercise stalled peers.
	writeTimeout time.Duration
}

// Server wraps the opamp-go server and manages workload state.
type Server struct {
	opamp    opampServer.OpAMPServer
	store    Store
	notifier Notifier

	registry     *InstanceRegistry
	grace        *GraceController
	retention    time.Duration
	now          func() time.Time
	writeTimeout time.Duration
	tokens       *tokenConnections

	mu    sync.RWMutex
	conns map[string]*tokenSession // instanceUID hex -> exact owning session

	stopMu    sync.Mutex
	stopState *serverStopState

	// pushFn sends a config YAML to a workload. Defaults to PushConfig;
	// overridable in tests so they can observe auto-push behavior without
	// wiring a real OpAMP connection.
	pushFn func(workloadID string, yaml []byte, targetInstanceUID string) error
}

type serverStopState struct {
	done chan struct{}
	err  error
}

// New creates a new OpAMP server. db and notifier can be nil (useful for
// testing).
func New(db Store, notifier Notifier, opts Options) *Server {
	if opts.DisconnectGrace <= 0 {
		opts.DisconnectGrace = 2 * time.Minute
	}
	if opts.RetentionDuration <= 0 {
		opts.RetentionDuration = 30 * 24 * time.Hour
	}
	if opts.now == nil {
		opts.now = time.Now
	}
	if opts.writeTimeout <= 0 || opts.writeTimeout > defaultWriteTimeout {
		opts.writeTimeout = defaultWriteTimeout
	}
	s := &Server{
		opamp:        opampServer.New(nil),
		store:        db,
		notifier:     notifier,
		registry:     NewInstanceRegistry(),
		grace:        NewGraceController(opts.DisconnectGrace),
		retention:    opts.RetentionDuration,
		now:          opts.now,
		writeTimeout: opts.writeTimeout,
		conns:        make(map[string]*tokenSession),
	}
	s.tokens = newTokenConnections(opts.now, opts.afterFunc, s.disconnectSessions)
	s.pushFn = func(workloadID string, yaml []byte, target string) error {
		return s.PushConfig(context.Background(), workloadID, yaml, target)
	}
	return s
}

// ConnectedInstanceCount returns the number of currently connected instances.
func (s *Server) ConnectedInstanceCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.conns)
}

// Instances returns a snapshot of the live instances bound to a workload.
// Exposed for the REST handler GET /api/workloads/:id/instances.
func (s *Server) Instances(workloadID string) []Instance {
	return s.registry.Instances(workloadID)
}

// InstanceWorkload returns the live workload binding for an instance UID.
func (s *Server) InstanceWorkload(instanceUID string) (string, bool) {
	return s.registry.LookupWorkload(instanceUID)
}

// GetConnection returns the OpAMP connection for a given instance UID, or nil.
func (s *Server) GetConnection(instanceUID string) types.Connection {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if session := s.conns[instanceUID]; session != nil {
		return session.conn
	}
	return nil
}

// PushConfig sends a remote config to one specific instance (when
// targetInstanceUID is set) or broadcasts it to every live instance of a
// workload.
func (s *Server) PushConfig(ctx context.Context, workloadID string, yamlContent []byte, targetInstanceUID string) error {
	configHash := sha256.Sum256(yamlContent)
	makeMsg := func(uid string) (*protobufs.ServerToAgent, error) {
		rawUID, err := hex.DecodeString(uid)
		if err != nil || len(rawUID) != 16 {
			return nil, fmt.Errorf("instance %s has invalid UID", uid)
		}
		return &protobufs.ServerToAgent{
			InstanceUid: rawUID,
			RemoteConfig: &protobufs.AgentRemoteConfig{
				Config: &protobufs.AgentConfigMap{
					ConfigMap: map[string]*protobufs.AgentConfigFile{
						"": {Body: yamlContent, ContentType: "text/yaml"},
					},
				},
				ConfigHash: configHash[:],
			},
		}, nil
	}

	if targetInstanceUID != "" {
		boundWorkloadID, ok := s.registry.LookupWorkload(targetInstanceUID)
		if !ok {
			return fmt.Errorf("instance %s not connected", targetInstanceUID)
		}
		if boundWorkloadID != workloadID {
			return fmt.Errorf("instance %s belongs to workload %s, not %s", targetInstanceUID, boundWorkloadID, workloadID)
		}
		msg, err := makeMsg(targetInstanceUID)
		if err != nil {
			return err
		}
		return s.sendToInstance(ctx, targetInstanceUID, msg)
	}

	instances := s.registry.Instances(workloadID)
	if len(instances) == 0 {
		return fmt.Errorf("workload %s has no connected instance", workloadID)
	}
	var firstErr error
	for _, i := range instances {
		msg, err := makeMsg(i.InstanceUID)
		if err == nil {
			err = s.sendToInstance(ctx, i.InstanceUID, msg)
		}
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (s *Server) sendToInstance(ctx context.Context, uid string, msg *protobufs.ServerToAgent) error {
	s.mu.RLock()
	session := s.conns[uid]
	s.mu.RUnlock()
	if session == nil {
		return fmt.Errorf("instance %s not connected", uid)
	}
	lease, ok := s.tokens.Acquire(session, s.now().UTC())
	if !ok {
		return fmt.Errorf("instance %s not connected", uid)
	}
	err := session.send(ctx, msg, s.writeTimeout)
	lease.Release()
	if err != nil {
		s.disconnectSession(session)
	}
	return err
}

func (session *tokenSession) send(ctx context.Context, msg *protobufs.ServerToAgent, timeout time.Duration) error {
	session.sendGate.Lock()
	defer session.sendGate.Unlock()
	_, err := session.sendWithDeadlineLocked(ctx, msg, timeout, nil)
	return err
}

func (session *tokenSession) sendHTTPResponse(
	ctx context.Context,
	msg *protobufs.ServerToAgent,
	timeout time.Duration,
	lease *tokenLease,
) (bool, error) {
	session.sendGate.Lock()
	retained, err := session.sendWithDeadlineLocked(ctx, msg, timeout, lease)
	if !retained {
		session.sendGate.Unlock()
	}
	return retained, err
}

// sendWithDeadlineLocked requires sendGate to be held. When it retains an HTTP
// response, ownership of both the lease and sendGate moves to httpLease and is
// released by finishHTTPResponse after the handler flushes the response.
func (session *tokenSession) sendWithDeadlineLocked(
	ctx context.Context,
	msg *protobufs.ServerToAgent,
	timeout time.Duration,
	httpLease *tokenLease,
) (bool, error) {
	conn := session.conn.Connection()
	if conn != nil {
		deadline := time.Now().Add(timeout)
		if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
			deadline = contextDeadline
		}
		if err := conn.SetWriteDeadline(deadline); err != nil {
			_ = conn.Close()
			return false, fmt.Errorf("set OpAMP write deadline: %w", err)
		}
	}
	err := session.conn.Send(ctx, msg)
	if errors.Is(err, opampServer.ErrInvalidHTTPConnection) && session.holdHTTPResponse(httpLease) {
		return true, err
	}
	if conn != nil {
		clearErr := conn.SetWriteDeadline(time.Time{})
		if clearErr != nil {
			_ = conn.Close()
			if err == nil {
				return false, fmt.Errorf("clear OpAMP write deadline: %w", clearErr)
			}
		}
	}
	return false, err
}

// Attach mounts the OpAMP handler on an existing HTTP mux.
func (s *Server) Attach() (opampServer.HTTPHandlerFunc, opampServer.ConnContext, error) {
	settings := opampServer.Settings{
		Callbacks: types.Callbacks{
			OnConnecting: s.authenticateRequest,
		},
	}

	handler, connContext, err := s.opamp.Attach(settings)
	if err != nil {
		return nil, nil, err
	}
	return func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("Content-Type") == "application/x-protobuf" {
			handler(&flushResponseWriter{ResponseWriter: w}, req)
			return
		}
		handler(w, req)
	}, connContext, nil
}

type flushResponseWriter struct {
	http.ResponseWriter
}

func (w *flushResponseWriter) Write(body []byte) (int, error) {
	written, err := w.ResponseWriter.Write(body)
	if err != nil {
		return written, err
	}
	if err := http.NewResponseController(w.ResponseWriter).Flush(); err != nil {
		return written, fmt.Errorf("flush OpAMP HTTP response: %w", err)
	}
	return written, nil
}

func (s *Server) authenticateRequest(req *http.Request) types.ConnectionResponse {
	const prefix = "Bearer "
	values := req.Header.Values("Authorization")
	if len(values) != 1 {
		return unauthorizedConnectionResponse()
	}
	auth := values[0]
	if !strings.HasPrefix(auth, prefix) || strings.Contains(auth, ",") {
		return unauthorizedConnectionResponse()
	}
	value := strings.TrimPrefix(auth, prefix)
	if value == "" || strings.ContainsAny(value, " \t\r\n") {
		return unauthorizedConnectionResponse()
	}
	id, presentedHash, err := opampauth.ParseAndHash(value)
	if err != nil {
		return unauthorizedConnectionResponse()
	}
	if s.store == nil {
		return serviceUnavailableConnectionResponse()
	}
	principal, err := s.store.ValidateOpAMPToken(req.Context(), id, presentedHash, s.now().UTC())
	if errors.Is(err, ext.ErrInvalidOpAMPToken) {
		return unauthorizedConnectionResponse()
	}
	if err != nil {
		return serviceUnavailableConnectionResponse()
	}
	session := &tokenSession{principal: principal}

	return types.ConnectionResponse{
		Accept:              true,
		ConnectionCallbacks: s.connectionCallbacks(session),
	}
}

func unauthorizedConnectionResponse() types.ConnectionResponse {
	return types.ConnectionResponse{
		Accept:             false,
		HTTPStatusCode:     http.StatusUnauthorized,
		HTTPResponseHeader: map[string]string{"WWW-Authenticate": `Bearer realm="opamp"`},
	}
}

func serviceUnavailableConnectionResponse() types.ConnectionResponse {
	return types.ConnectionResponse{
		Accept:         false,
		HTTPStatusCode: http.StatusServiceUnavailable,
	}
}

// Stop gracefully shuts down the OpAMP server. A nil result means all cleanup
// completed. If ctx expires, cleanup continues and a later Stop call can join
// the same shutdown to completion.
func (s *Server) Stop(ctx context.Context) error {
	state := s.beginStop()
	select {
	case <-state.done:
		return state.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Server) beginStop() *serverStopState {
	s.stopMu.Lock()
	if s.stopState != nil {
		state := s.stopState
		s.stopMu.Unlock()
		return state
	}
	graceState := s.grace.beginStop()
	tokenState, _ := s.tokens.beginStop()
	state := &serverStopState{done: make(chan struct{})}
	s.stopState = state
	s.stopMu.Unlock()

	go func() {
		<-graceState.done
		<-tokenState.done
		state.err = s.opamp.Stop(context.Background())
		close(state.done)
	}()
	return state
}

// flattenAttrs merges identifying and non-identifying OpAMP attributes into a
// flat map[string]string, skipping non-string and empty values.
func flattenAttrs(identifying, nonIdentifying []*protobufs.KeyValue) map[string]string {
	out := make(map[string]string, len(identifying)+len(nonIdentifying))
	for _, kv := range identifying {
		if kv == nil || kv.Value == nil {
			continue
		}
		if v := kv.Value.GetStringValue(); v != "" {
			out[kv.Key] = v
		}
	}
	for _, kv := range nonIdentifying {
		if kv == nil || kv.Value == nil {
			continue
		}
		if v := kv.Value.GetStringValue(); v != "" {
			out[kv.Key] = v
		}
	}
	return out
}

type agentMessageKind int

const (
	agentMessageWithDescription agentMessageKind = iota
	agentMessageHeartbeat
)

func classifyAgentMessage(msg *protobufs.AgentToServer) agentMessageKind {
	if msg.AgentDescription != nil {
		return agentMessageWithDescription
	}
	return agentMessageHeartbeat
}

// handleAcceptedMessage applies an already authenticated and admitted OpAMP
// message. Keeping authentication and connection ownership outside this helper
// lets the historical workload tests exercise message semantics directly.
func (s *Server) handleAcceptedMessage(msg *protobufs.AgentToServer) *protobufs.ServerToAgent {
	uid := hex.EncodeToString(msg.InstanceUid)
	var workloadID string
	var requestComponents bool

	switch classifyAgentMessage(msg) {
	case agentMessageWithDescription:
		workloadID, requestComponents = s.handleAgentDescription(uid, msg)
	case agentMessageHeartbeat:
		var known bool
		workloadID, known = s.handleKnownInstanceUpdate(uid, msg)
		if !known {
			return reportFullStateReply(msg)
		}
	}

	s.refreshWorkloadState(workloadID, uid)
	s.recordRemoteConfigStatus(workloadID, uid, msg.RemoteConfigStatus)

	return replyToAgent(msg, requestComponents)
}

func (s *Server) connectionCallbacks(session *tokenSession) types.ConnectionCallbacks {
	return types.ConnectionCallbacks{
		OnConnected: func(_ context.Context, conn types.Connection) {
			if !s.tokens.Track(session, conn) {
				if err := conn.Disconnect(); err != nil && !errors.Is(err, opampServer.ErrInvalidHTTPConnection) {
					log.Printf("OpAMP connection rejected after authentication: %v", err)
				}
			}
		},
		OnMessage: func(ctx context.Context, conn types.Connection, msg *protobufs.AgentToServer) *protobufs.ServerToAgent {
			return s.onSessionMessage(ctx, session, conn, msg)
		},
		OnConnectionClose: func(conn types.Connection) {
			s.onSessionConnectionClose(session, conn)
		},
	}
}

func (s *Server) onSessionMessage(
	ctx context.Context,
	session *tokenSession,
	conn types.Connection,
	msg *protobufs.AgentToServer,
) *protobufs.ServerToAgent {
	lease, ok := s.tokens.Acquire(session, s.now().UTC())
	if !ok {
		return nil
	}

	session.messageGate.Lock()
	if session.terminal {
		session.messageGate.Unlock()
		lease.Release()
		return nil
	}
	reply, disconnect := s.processSessionMessage(ctx, session, conn, msg)
	if disconnect {
		session.terminal = true
	}
	session.messageGate.Unlock()
	if disconnect {
		lease.Release()
		s.disconnectSession(session)
		return nil
	}
	if reply == nil {
		lease.Release()
		return nil
	}

	// opamp-go normally sends this reply after OnMessage returns. WebSockets
	// instead send it here so the token lease covers the complete operation.
	// Plain HTTP cannot use Connection.Send; returning the reply is its only
	// supported response path.
	retained, err := session.sendHTTPResponse(ctx, reply, s.writeTimeout, lease)
	if retained {
		return reply
	}
	lease.Release()
	if errors.Is(err, opampServer.ErrInvalidHTTPConnection) {
		s.tokens.Remove(session)
		s.tokens.waitRemoveDrain(session)
		s.disconnectSessions([]*tokenSession{session})
		s.tokens.CompleteRemove(session)
		return nil
	}
	if err != nil {
		s.disconnectSession(session)
	}
	return nil
}

func (s *Server) processSessionMessage(
	ctx context.Context,
	session *tokenSession,
	conn types.Connection,
	msg *protobufs.AgentToServer,
) (*protobufs.ServerToAgent, bool) {
	if msg == nil || len(msg.InstanceUid) != 16 || session.conn != conn {
		return nil, true
	}
	uid := hex.EncodeToString(msg.InstanceUid)

	s.mu.Lock()
	if session.admitted {
		matches := session.uid == uid && s.conns[uid] == session
		s.mu.Unlock()
		if !matches {
			return nil, true
		}
		return s.handleAcceptedMessage(msg), false
	}
	if session.uid != "" {
		s.mu.Unlock()
		return nil, true
	}
	if owner := s.conns[uid]; owner != nil && owner != session {
		s.mu.Unlock()
		return nil, true
	}
	session.uid = uid
	s.conns[uid] = session
	s.mu.Unlock()

	if s.store == nil || s.store.MarkOpAMPTokenUsed(ctx, session.principal.ID, s.now().UTC()) != nil {
		return nil, true
	}

	s.mu.Lock()
	if s.conns[uid] != session {
		s.mu.Unlock()
		return nil, true
	}
	session.admitted = true
	s.mu.Unlock()
	return s.handleAcceptedMessage(msg), false
}

func (s *Server) onSessionConnectionClose(session *tokenSession, _ types.Connection) {
	// Remove first, even for a connection that closed before its first message.
	// Remove is non-blocking so a close callback cannot deadlock with a Send that
	// is being unblocked by transport shutdown.
	s.tokens.Remove(session)
	session.finishHTTPResponse()
	s.tokens.waitRemoveDrain(session)
	s.releaseSession(session)
	s.tokens.CompleteRemove(session)
}

func (s *Server) handleAgentDescription(uid string, msg *protobufs.AgentToServer) (string, bool) {
	desc := msg.AgentDescription
	attrs := flattenAttrs(desc.IdentifyingAttributes, desc.NonIdentifyingAttributes)
	fp := Fingerprint(attrs, uid)
	workloadID := fp.ID

	version := attrs["service.version"]
	// Capture the previous version BEFORE BindInstance overwrites it so
	// we can emit a version_changed event on rebind.
	prevVersion, _ := s.registry.PreviousVersion(uid)

	ins := Instance{
		PodName:                     attrs["k8s.pod.name"],
		Version:                     version,
		Healthy:                     true,
		AcceptsRemoteConfig:         msg.Capabilities&acceptsRemoteConfigCap != 0,
		RemoteConfigCapabilityKnown: true,
	}
	if msg.Health != nil && !msg.Health.Healthy {
		ins.Healthy = false
	}
	if msg.RemoteConfigStatus != nil {
		ins.EffectiveConfigHash = hex.EncodeToString(msg.RemoteConfigStatus.LastRemoteConfigHash)
	}

	isFresh := s.registry.BindInstance(uid, workloadID, ins)
	// A new binding supersedes any pending grace timer.
	s.grace.Cancel(workloadID)

	// Upsert the workload row BEFORE emitting events — workload_events
	// has a FK to workloads(id) so the parent must exist first.
	s.upsertWorkloadFromDescription(uid, workloadID, fp, attrs, msg)
	s.emitBindEvent(workloadID, uid, ins, isFresh, prevVersion, version)

	return workloadID, s.shouldRequestAvailableComponents(workloadID, msg)
}

func (s *Server) emitBindEvent(workloadID, uid string, ins Instance, isFresh bool, prevVersion, version string) {
	if isFresh {
		s.emitEvent(models.WorkloadEvent{
			WorkloadID:  workloadID,
			InstanceUID: uid,
			PodName:     ins.PodName,
			EventType:   "connected",
			Version:     ins.Version,
			OccurredAt:  time.Now().UTC(),
		})
		return
	}
	if prevVersion != "" && prevVersion != version {
		s.emitEvent(models.WorkloadEvent{
			WorkloadID:  workloadID,
			InstanceUID: uid,
			PodName:     ins.PodName,
			EventType:   "version_changed",
			Version:     version,
			PrevVersion: prevVersion,
			OccurredAt:  time.Now().UTC(),
		})
	}
}

func (s *Server) shouldRequestAvailableComponents(workloadID string, msg *protobufs.AgentToServer) bool {
	// Only ask for available_components when the capability bit is set and the
	// agent hasn't already populated the list.
	if msg.Capabilities&reportsAvailableComponentsCap == 0 {
		return false
	}
	wl, err := s.getWorkload(workloadID)
	return err == nil && wl.AvailableComponents == nil
}

func (s *Server) handleKnownInstanceUpdate(uid string, msg *protobufs.AgentToServer) (string, bool) {
	wl, ok := s.registry.LookupWorkload(uid)
	if !ok {
		return "", false
	}
	s.registry.UpdateInstance(uid, func(i *Instance) {
		if msg.Health != nil {
			i.Healthy = msg.Health.Healthy
		}
		if msg.RemoteConfigStatus != nil {
			i.EffectiveConfigHash = hex.EncodeToString(msg.RemoteConfigStatus.LastRemoteConfigHash)
		}
	})
	return wl, true
}

func (s *Server) refreshWorkloadState(workloadID, uid string) {
	// Aggregated status + broadcast + conditional auto-push.
	if s.store == nil {
		return
	}
	wl, err := s.store.GetWorkload(workloadID)
	if err != nil {
		return
	}
	wl.Status = s.registry.AggregatedStatus(workloadID)
	wl.LastSeenAt = time.Now().UTC()
	if err := s.store.UpsertWorkload(wl); err != nil {
		log.Printf("Failed to upsert workload %s: %v", workloadID, err)
	}
	if s.notifier != nil {
		connected := s.registry.Count(workloadID)
		drifted := s.countDrift(workloadID, wl.ActiveConfigHash)
		s.notifier.BroadcastWorkloadUpdate(wl, connected, drifted)
	}
	s.maybeTriggerAutoPush(workloadID, uid, wl)
}

func (s *Server) maybeTriggerAutoPush(workloadID, uid string, wl models.Workload) {
	// Auto-push (P.2): only when this specific instance diverges from the
	// workload's pinned active config.
	if wl.ActiveConfigHash == "" || wl.ActiveConfigID == nil {
		return
	}
	for _, i := range s.registry.Instances(workloadID) {
		if i.InstanceUID != uid {
			continue
		}
		if i.EffectiveConfigHash != "" && i.EffectiveConfigHash != wl.ActiveConfigHash {
			//nolint:gosec // auto-push is server-initiated and must outlive the OpAMP message context
			go s.triggerAutoPush(context.Background(), *wl.ActiveConfigID, workloadID, uid)
		}
	}
}

func (s *Server) recordRemoteConfigStatus(workloadID, uid string, status *protobufs.RemoteConfigStatus) {
	// RemoteConfigStatus bookkeeping (keeps the audit trail in workload_configs
	// + auto-rollback on FAILED).
	if s.store == nil || status == nil {
		return
	}
	s.handleRemoteConfigStatus(workloadID, uid, status)
}

func reportFullStateReply(msg *protobufs.AgentToServer) *protobufs.ServerToAgent {
	// Unknown UID with no AgentDescription — either a fresh agent that hasn't
	// identified yet, or an existing agent reconnecting after we lost registry
	// state (server restart with ephemeral DB). Ask for a full state so we can
	// bootstrap the workload; OpAMP agents won't resend AgentDescription on their
	// own.
	return &protobufs.ServerToAgent{
		InstanceUid: msg.InstanceUid,
		Flags:       uint64(protobufs.ServerToAgentFlags_ServerToAgentFlags_ReportFullState),
	}
}

func replyToAgent(msg *protobufs.AgentToServer, requestComponents bool) *protobufs.ServerToAgent {
	reply := &protobufs.ServerToAgent{InstanceUid: msg.InstanceUid}
	if requestComponents {
		reply.Flags = uint64(protobufs.ServerToAgentFlags_ServerToAgentFlags_ReportAvailableComponents)
	}
	return reply
}

// getWorkload is a nil-safe wrapper around Store.GetWorkload.
func (s *Server) getWorkload(id string) (models.Workload, error) {
	if s.store == nil {
		return models.Workload{}, fmt.Errorf("no store")
	}
	return s.store.GetWorkload(id)
}

// upsertWorkloadFromDescription materializes the workload row from the live
// attributes, merging with DB state so we don't clobber fields managed
// elsewhere (active_config_id, retention_until, remote_config_status snap).
func (s *Server) upsertWorkloadFromDescription(_, workloadID string, fp FingerprintResult, attrs map[string]string, msg *protobufs.AgentToServer) {
	if s.store == nil {
		return
	}
	var w models.Workload
	if prev, err := s.store.GetWorkload(workloadID); err == nil {
		w = prev
	}
	w.ID = workloadID
	w.FingerprintSource = fp.Source
	w.FingerprintKeys = models.FingerprintKeys(fp.Keys)
	if svc := attrs["service.name"]; svc != "" {
		w.DisplayName = svc
	}
	if v := attrs["service.version"]; v != "" {
		w.Version = v
	}
	w.Type = classifyAgent(attrs)
	w.Status = s.registry.AggregatedStatus(workloadID)
	w.LastSeenAt = time.Now().UTC()
	w.AcceptsRemoteConfig = msg.Capabilities&acceptsRemoteConfigCap != 0

	// Rebuild labels from scratch to match the current attribute set. Skip
	// keys already projected into dedicated columns. Preserve reserved trusted
	// selector labels only from existing DB state; never accept them from
	// self-reported OpAMP attributes.
	trustedLabels := models.Labels{}
	for k, v := range w.Labels {
		if strings.HasPrefix(k, models.TrustedSelectorLabelPrefix) {
			trustedLabels[k] = v
		}
	}
	w.Labels = models.Labels{}
	for k, v := range trustedLabels {
		w.Labels[k] = v
	}
	for k, v := range attrs {
		switch k {
		case "service.name", "service.version":
			continue
		}
		if strings.HasPrefix(k, models.TrustedSelectorLabelPrefix) {
			continue
		}
		w.Labels[k] = v
	}

	// Resurrection: a live message clears any retention deadline.
	w.RetentionUntil = nil

	if cfgID := s.persistEffectiveConfig(workloadID, w.DisplayName, msg.EffectiveConfig); cfgID != "" {
		w.ActiveConfigID = &cfgID
	}

	if ac := flattenAvailableComponents(msg.AvailableComponents); ac != nil {
		w.AvailableComponents = ac
	}

	if w.Type == "" {
		return
	}
	if err := s.store.UpsertWorkload(w); err != nil {
		log.Printf("Failed to upsert workload %s: %v", workloadID, err)
		return
	}
	// The UPSERT already wrote retention_until=NULL via the w.RetentionUntil=nil
	// assignment above, but an explicit clear is cheap and guards against
	// future schema changes that might stop propagating NULL through COALESCE.
	if err := s.store.ClearWorkloadRetention(workloadID); err != nil {
		log.Printf("clear retention %s: %v", workloadID, err)
	}
}

// countDrift returns how many live instances have an effective config hash
// that differs from the workload's pinned active hash.
func (s *Server) countDrift(workloadID, activeHash string) int {
	if activeHash == "" {
		return 0
	}
	n := 0
	for _, i := range s.registry.Instances(workloadID) {
		if i.EffectiveConfigHash != "" && i.EffectiveConfigHash != activeHash {
			n++
		}
	}
	return n
}

// triggerAutoPush re-pushes the workload's pinned config to a single instance
// that has reported a divergent effective hash. Runs as a goroutine launched
// from onMessage, so all errors are logged (no channel to propagate them).
func (s *Server) triggerAutoPush(_ context.Context, configID, workloadID, instanceUID string) {
	if s.store == nil {
		return
	}
	cfg, err := s.store.GetConfig(configID)
	if err != nil {
		log.Printf("auto-push: cannot load config %s: %v", configID, err)
		return
	}
	if err := s.pushFn(workloadID, []byte(cfg.Content), instanceUID); err != nil {
		log.Printf("auto-push to workload=%s instance=%s failed: %v", workloadID, instanceUID, err)
	}
}

// emitEvent persists a WorkloadEvent and broadcasts it. Store failures are
// logged — events are best-effort.
func (s *Server) emitEvent(e models.WorkloadEvent) {
	if s.store == nil {
		return
	}
	id, err := s.store.InsertWorkloadEvent(e)
	if err != nil {
		log.Printf("workload_events insert: %v", err)
		return
	}
	e.ID = id
	if s.notifier != nil {
		s.notifier.BroadcastWorkloadEvent(e)
	}
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

// DisconnectTokenConnections tombstones a managed token, waits for operations
// admitted before the tombstone, then releases and closes only its sessions.
func (s *Server) DisconnectTokenConnections(tokenID string) int {
	sessions := s.tokens.Disable(tokenID)
	return len(sessions)
}

func (s *Server) disconnectSessions(sessions []*tokenSession) {
	for _, session := range sessions {
		session.disconnectOnce.Do(func() {
			s.releaseSession(session)
			if session.conn == nil {
				return
			}
			if err := session.conn.Disconnect(); err != nil && !errors.Is(err, opampServer.ErrInvalidHTTPConnection) {
				log.Printf("OpAMP connection disconnect failed: %v", err)
			}
		})
	}
}

func (s *Server) disconnectSession(session *tokenSession) {
	s.tokens.Remove(session)
	s.tokens.waitRemoveDrain(session)
	s.disconnectSessions([]*tokenSession{session})
	s.tokens.CompleteRemove(session)
}

func (s *Server) releaseSession(session *tokenSession) {
	if session == nil {
		return
	}
	session.releaseOnce.Do(func() {
		s.releaseSessionOnce(session)
	})
}

func (s *Server) releaseSessionOnce(session *tokenSession) {

	// Keep the exact owner check and registry unbind in one critical section.
	// A late close from an older connection can therefore never unbind a newer
	// session that has claimed the same UID.
	s.mu.Lock()
	uid := session.uid
	if uid == "" || s.conns[uid] != session {
		s.mu.Unlock()
		return
	}
	delete(s.conns, uid)
	session.uid = ""
	session.admitted = false

	var podName string
	if wl, found := s.registry.LookupWorkload(uid); found {
		for _, i := range s.registry.Instances(wl) {
			if i.InstanceUID == uid {
				podName = i.PodName
				break
			}
		}
	}
	workloadID := s.registry.UnbindInstance(uid)
	s.mu.Unlock()
	if workloadID == "" {
		return
	}

	s.emitEvent(models.WorkloadEvent{
		WorkloadID:  workloadID,
		InstanceUID: uid,
		PodName:     podName,
		EventType:   "disconnected",
		OccurredAt:  time.Now().UTC(),
	})

	if s.registry.Count(workloadID) == 0 {
		s.grace.Schedule(workloadID, func() {
			// Re-check under the real clock: a rolling restart could have
			// rebound an instance during the grace window.
			if s.registry.Count(workloadID) > 0 {
				return
			}
			if s.store == nil {
				return
			}
			until := time.Now().UTC().Add(s.retention)
			if err := s.store.MarkWorkloadDisconnected(workloadID, until); err != nil {
				log.Printf("mark disconnected %s: %v", workloadID, err)
				return
			}
			if s.notifier != nil {
				if wl, err := s.store.GetWorkload(workloadID); err == nil {
					s.notifier.BroadcastWorkloadUpdate(wl, 0, 0)
				}
			}
		})
	}
}

// persistEffectiveConfig stores the YAML config reported by the agent
// (deduplicated by content hash) and returns the resulting config ID.
// Returns empty if the message carries no effective config (typical for
// heartbeats).
func (s *Server) persistEffectiveConfig(workloadID, displayName string, effective *protobufs.EffectiveConfig) string {
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
		shortFallback := workloadID
		if len(shortFallback) > 8 {
			shortFallback = shortFallback[:8]
		}
		name := fmt.Sprintf("%s-reported-%s", fallback(displayName, shortFallback), configID[:8])
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

// classifyAgent decides whether a workload is an OTel Collector or an SDK,
// based on identifying attributes. The order of checks is significant:
// otelcol.version is authoritative when present; os.description containing
// "otelcol" catches collectors that omit it; telemetry.sdk.language is
// authoritative for SDKs; the service.name fallback preserves backward
// compatibility with agents that report none of the above.
func classifyAgent(attrs map[string]string) string {
	if attrs["otelcol.version"] != "" {
		return "collector"
	}
	if strings.Contains(strings.ToLower(attrs["os.description"]), "otelcol") {
		return "collector"
	}
	if attrs["telemetry.sdk.language"] != "" {
		return "sdk"
	}
	if isCollectorName(attrs["service.name"]) {
		return "collector"
	}
	return "sdk"
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
