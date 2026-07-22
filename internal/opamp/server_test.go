package opamp

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/open-telemetry/opamp-go/protobufs"
	opampServer "github.com/open-telemetry/opamp-go/server"
	"github.com/open-telemetry/opamp-go/server/types"
	"google.golang.org/protobuf/proto"

	"github.com/magnify-labs/otel-magnify/internal/opampauth"
	"github.com/magnify-labs/otel-magnify/pkg/ext"
	"github.com/magnify-labs/otel-magnify/pkg/models"
)

type recordingConn struct {
	mu sync.Mutex

	netConn          net.Conn
	sent             []*protobufs.ServerToAgent
	sendsInFlight    int
	maxSendsInFlight int
	disconnectErr    error
	disconnectCount  int
	disconnectHook   func()
	sendStarted      chan struct{}
	sendRelease      chan struct{}
	sendErr          error
}

func (c *recordingConn) Connection() net.Conn { return c.netConn }
func (c *recordingConn) Disconnect() error {
	c.mu.Lock()
	c.disconnectCount++
	err := c.disconnectErr
	hook := c.disconnectHook
	c.mu.Unlock()
	if hook != nil {
		hook()
	}
	return err
}
func (c *recordingConn) Send(_ context.Context, msg *protobufs.ServerToAgent) error {
	c.mu.Lock()
	c.sendsInFlight++
	if c.sendsInFlight > c.maxSendsInFlight {
		c.maxSendsInFlight = c.sendsInFlight
	}
	c.mu.Unlock()
	if c.sendStarted != nil {
		select {
		case c.sendStarted <- struct{}{}:
		default:
		}
	}
	if c.sendRelease != nil {
		<-c.sendRelease
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sendsInFlight--
	if c.sendErr != nil {
		return c.sendErr
	}
	c.sent = append(c.sent, msg)
	return nil
}

func (c *recordingConn) maxConcurrentSends() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.maxSendsInFlight
}

type writeDeadlineConn struct {
	net.Conn
	mu        sync.Mutex
	deadlines []time.Time
	failSet   error
	failClear error
	closed    chan struct{}
	closeOnce sync.Once
}

func (c *writeDeadlineConn) SetWriteDeadline(deadline time.Time) error {
	c.mu.Lock()
	c.deadlines = append(c.deadlines, deadline)
	var injectedErr error
	if deadline.IsZero() {
		injectedErr = c.failClear
	} else {
		injectedErr = c.failSet
	}
	c.mu.Unlock()
	if injectedErr != nil {
		return injectedErr
	}
	return c.Conn.SetWriteDeadline(deadline)
}

func (c *writeDeadlineConn) Close() error {
	c.closeOnce.Do(func() {
		if c.closed != nil {
			close(c.closed)
		}
	})
	return c.Conn.Close()
}

func (c *writeDeadlineConn) injectDeadlineErrors(setErr, clearErr error) {
	c.mu.Lock()
	c.failSet = setErr
	c.failClear = clearErr
	c.mu.Unlock()
}

func (c *writeDeadlineConn) resetDeadlines() {
	c.mu.Lock()
	c.deadlines = nil
	c.mu.Unlock()
}

func (c *writeDeadlineConn) deadlineSnapshot() []time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]time.Time(nil), c.deadlines...)
}

type blockingResponseWriter struct {
	header       http.Header
	writeStarted chan struct{}
	writeRelease chan struct{}
	startOnce    sync.Once
}

func newBlockingResponseWriter() *blockingResponseWriter {
	return &blockingResponseWriter{
		header:       make(http.Header),
		writeStarted: make(chan struct{}),
		writeRelease: make(chan struct{}),
	}
}

func (w *blockingResponseWriter) Header() http.Header { return w.header }
func (*blockingResponseWriter) WriteHeader(_ int)     {}
func (w *blockingResponseWriter) Write(body []byte) (int, error) {
	w.startOnce.Do(func() { close(w.writeStarted) })
	<-w.writeRelease
	return len(body), nil
}

type blockingFlushResponseWriter struct {
	header       http.Header
	writeStarted chan struct{}
	flushStarted chan struct{}
	flushRelease chan struct{}
	writeOnce    sync.Once
	flushOnce    sync.Once
}

func newBlockingFlushResponseWriter() *blockingFlushResponseWriter {
	return &blockingFlushResponseWriter{
		header:       make(http.Header),
		writeStarted: make(chan struct{}),
		flushStarted: make(chan struct{}),
		flushRelease: make(chan struct{}),
	}
}

func (w *blockingFlushResponseWriter) Header() http.Header { return w.header }
func (*blockingFlushResponseWriter) WriteHeader(_ int)     {}
func (w *blockingFlushResponseWriter) Write(body []byte) (int, error) {
	w.writeOnce.Do(func() { close(w.writeStarted) })
	return len(body), nil
}
func (w *blockingFlushResponseWriter) Flush() {
	w.flushOnce.Do(func() { close(w.flushStarted) })
	<-w.flushRelease
}

type pipeWritingConn struct {
	conn           net.Conn
	sendStarted    chan struct{}
	sendOnce       sync.Once
	disconnectOnce sync.Once
	disconnected   chan struct{}
}

func newPipeWritingConn(conn net.Conn) *pipeWritingConn {
	return &pipeWritingConn{
		conn:         conn,
		sendStarted:  make(chan struct{}),
		disconnected: make(chan struct{}),
	}
}

func (c *pipeWritingConn) Connection() net.Conn { return c.conn }
func (c *pipeWritingConn) Disconnect() error {
	var err error
	c.disconnectOnce.Do(func() {
		close(c.disconnected)
		err = c.conn.Close()
	})
	return err
}
func (c *pipeWritingConn) Send(_ context.Context, _ *protobufs.ServerToAgent) error {
	c.sendOnce.Do(func() { close(c.sendStarted) })
	_, err := c.conn.Write([]byte{0x01})
	return err
}

func (c *recordingConn) disconnects() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.disconnectCount
}

func (c *recordingConn) sentCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.sent)
}

func (c *recordingConn) onlyMessage() *protobufs.ServerToAgent {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.sent) != 1 {
		return nil
	}
	return c.sent[0]
}

func TestNewOpAMPServer(t *testing.T) {
	srv := New(nil, nil, Options{})
	if srv == nil {
		t.Fatal("New returned nil")
	}
}

func TestIsCollectorName(t *testing.T) {
	collectors := []string{
		"otelcol",
		"otelcol-contrib",
		"otelcol-custom",
		"OtelCol-Contrib",
		"io.opentelemetry.collector",
		"my-opentelemetry-collector",
	}
	for _, name := range collectors {
		if !isCollectorName(name) {
			t.Errorf("isCollectorName(%q) = false, want true", name)
		}
	}

	sdks := []string{
		"my-service",
		"payment-api",
		"",
		"flask-app",
	}
	for _, name := range sdks {
		if isCollectorName(name) {
			t.Errorf("isCollectorName(%q) = true, want false", name)
		}
	}
}

func TestClassifyAgent_CollectorByOtelcolVersion(t *testing.T) {
	attrs := map[string]string{
		"otelcol.version": "0.150.1",
		"service.name":    "my-custom-collector",
	}
	if got := classifyAgent(attrs); got != "collector" {
		t.Errorf("classifyAgent(%v) = %q, want %q", attrs, got, "collector")
	}
}

func TestClassifyAgent_CollectorByOsDescription(t *testing.T) {
	attrs := map[string]string{
		"os.description": "otelcol/0.150.1 (linux/amd64)",
	}
	if got := classifyAgent(attrs); got != "collector" {
		t.Errorf("classifyAgent(%v) = %q, want %q", attrs, got, "collector")
	}
}

func TestClassifyAgent_SDKByLanguage(t *testing.T) {
	attrs := map[string]string{
		"telemetry.sdk.language": "go",
		"service.name":           "otelcol-trap",
	}
	if got := classifyAgent(attrs); got != "sdk" {
		t.Errorf("classifyAgent(%v) = %q, want %q", attrs, got, "sdk")
	}
}

func TestClassifyAgent_FallbackByServiceName_Collector(t *testing.T) {
	attrs := map[string]string{
		"service.name": "otelcol-foo",
	}
	if got := classifyAgent(attrs); got != "collector" {
		t.Errorf("classifyAgent(%v) = %q, want %q", attrs, got, "collector")
	}
}

func TestClassifyAgent_FallbackByServiceName_SDK(t *testing.T) {
	attrs := map[string]string{
		"service.name": "my-app",
	}
	if got := classifyAgent(attrs); got != "sdk" {
		t.Errorf("classifyAgent(%v) = %q, want %q", attrs, got, "sdk")
	}
}

func TestInstanceCountStartsZero(t *testing.T) {
	srv := New(nil, nil, Options{})
	if srv == nil {
		t.Fatal("New returned nil")
	}
	if srv.ConnectedInstanceCount() != 0 {
		t.Errorf("expected 0 connected instances, got %d", srv.ConnectedInstanceCount())
	}
}

func TestClassifyAgentMessage(t *testing.T) {
	tests := []struct {
		name string
		msg  *protobufs.AgentToServer
		want agentMessageKind
	}{
		{
			name: "full state includes agent description",
			msg: &protobufs.AgentToServer{
				AgentDescription: &protobufs.AgentDescription{},
			},
			want: agentMessageWithDescription,
		},
		{
			name: "remote config status without description is heartbeat",
			msg: &protobufs.AgentToServer{
				RemoteConfigStatus: &protobufs.RemoteConfigStatus{},
			},
			want: agentMessageHeartbeat,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyAgentMessage(tt.msg); got != tt.want {
				t.Fatalf("classifyAgentMessage() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPushConfig_TargetInstanceSendsOnlyToThatConnection(t *testing.T) {
	srv := New(nil, nil, Options{})
	uidA := "000102030405060708090a0b0c0d0e0f"
	uidB := "101112131415161718191a1b1c1d1e1f"
	srv.registry.BindInstance(uidA, "w1", Instance{Healthy: true})
	srv.registry.BindInstance(uidB, "w1", Instance{Healthy: true})
	connA := &recordingConn{}
	connB := &recordingConn{}
	sessionA := &tokenSession{principal: models.OpAMPTokenPrincipal{ID: "token-a"}, conn: connA, uid: uidA, admitted: true}
	sessionB := &tokenSession{principal: models.OpAMPTokenPrincipal{ID: "token-b"}, conn: connB, uid: uidB, admitted: true}
	if !srv.tokens.Track(sessionA, connA) || !srv.tokens.Track(sessionB, connB) {
		t.Fatal("failed to track push test sessions")
	}
	srv.mu.Lock()
	srv.conns[uidA] = sessionA
	srv.conns[uidB] = sessionB
	srv.mu.Unlock()

	if err := srv.PushConfig(context.Background(), "w1", []byte("receivers: {}"), uidB); err != nil {
		t.Fatalf("PushConfig target: %v", err)
	}

	if got := connA.sentCount(); got != 0 {
		t.Fatalf("uid-a received %d messages, want 0", got)
	}
	msg := connB.onlyMessage()
	if msg == nil {
		t.Fatalf("uid-b messages = %d, want 1", connB.sentCount())
	}
	if got := hex.EncodeToString(msg.InstanceUid); got != uidB {
		t.Fatalf("message InstanceUid = %q, want raw UID %q", got, uidB)
	}
}

func TestPushConfig_TargetInstanceRejectsCrossWorkloadBinding(t *testing.T) {
	srv := New(nil, nil, Options{})
	uidA := "202122232425262728292a2b2c2d2e2f"
	uidOther := "303132333435363738393a3b3c3d3e3f"
	srv.registry.BindInstance(uidA, "w1", Instance{Healthy: true})
	srv.registry.BindInstance(uidOther, "w2", Instance{Healthy: true})
	connOther := &recordingConn{}
	session := &tokenSession{principal: models.OpAMPTokenPrincipal{ID: "token-other"}, conn: connOther, uid: uidOther, admitted: true}
	if !srv.tokens.Track(session, connOther) {
		t.Fatal("failed to track cross-workload test session")
	}
	srv.mu.Lock()
	srv.conns[uidOther] = session
	srv.mu.Unlock()

	if err := srv.PushConfig(context.Background(), "w1", []byte("receivers: {}"), uidOther); err == nil {
		t.Fatal("PushConfig accepted a target instance bound to another workload")
	}

	if got := connOther.sentCount(); got != 0 {
		t.Fatalf("cross-workload target received %d messages, want 0", got)
	}
}

type managedTokenStore struct {
	*fakeStore

	tokenMu       sync.Mutex
	expectedID    string
	expectedHash  [32]byte
	expiresAt     *time.Time
	validateErr   error
	markErr       error
	validateCalls int
	markCalls     int
	markStarted   chan struct{}
	markRelease   chan struct{}
}

func (s *managedTokenStore) ValidateOpAMPToken(
	_ context.Context,
	id string,
	presentedHash [32]byte,
	_ time.Time,
) (models.OpAMPTokenPrincipal, error) {
	s.tokenMu.Lock()
	s.validateCalls++
	err := s.validateErr
	expectedID := s.expectedID
	expectedHash := s.expectedHash
	expiresAt := s.expiresAt
	s.tokenMu.Unlock()
	if err != nil {
		return models.OpAMPTokenPrincipal{}, err
	}
	if id != expectedID || presentedHash != expectedHash {
		return models.OpAMPTokenPrincipal{}, ext.ErrInvalidOpAMPToken
	}
	return models.OpAMPTokenPrincipal{ID: expectedID, ExpiresAt: expiresAt}, nil
}

func (s *managedTokenStore) MarkOpAMPTokenUsed(_ context.Context, id string, _ time.Time) error {
	s.tokenMu.Lock()
	s.markCalls++
	err := s.markErr
	started := s.markStarted
	release := s.markRelease
	expectedID := s.expectedID
	s.tokenMu.Unlock()
	if id != expectedID {
		return ext.ErrInvalidOpAMPToken
	}
	if started != nil {
		select {
		case started <- struct{}{}:
		default:
		}
	}
	if release != nil {
		<-release
	}
	return err
}

func (s *managedTokenStore) counts() (validate, mark int) {
	s.tokenMu.Lock()
	defer s.tokenMu.Unlock()
	return s.validateCalls, s.markCalls
}

func newManagedTokenServer(t *testing.T, expiresAt *time.Time, opts Options) (*Server, *managedTokenStore, opampauth.GeneratedToken) {
	t.Helper()
	generated, err := opampauth.Generate()
	if err != nil {
		t.Fatalf("generate managed token: %v", err)
	}
	store := &managedTokenStore{
		fakeStore:    newFakeStore(),
		expectedID:   generated.ID,
		expectedHash: generated.SecretHash,
		expiresAt:    expiresAt,
	}
	return New(store, nil, opts), store, generated
}

func managedTokenRequest(value string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/v1/opamp", nil)
	if value != "" {
		req.Header.Add("Authorization", value)
	}
	return req
}

func assertGenericUnauthorized(t *testing.T, response types.ConnectionResponse) {
	t.Helper()
	if response.Accept {
		t.Fatal("invalid OpAMP credentials were accepted")
	}
	if response.HTTPStatusCode != http.StatusUnauthorized {
		t.Fatalf("HTTP status = %d, want 401", response.HTTPStatusCode)
	}
	if got := response.HTTPResponseHeader["WWW-Authenticate"]; got != `Bearer realm="opamp"` {
		t.Fatalf("WWW-Authenticate = %q, want generic Bearer challenge", got)
	}
}

func authenticateManagedToken(t *testing.T, server *Server, value string) types.ConnectionCallbacks {
	t.Helper()
	response := server.authenticateRequest(managedTokenRequest("Bearer " + value))
	if !response.Accept {
		t.Fatalf("valid managed token rejected with HTTP status %d", response.HTTPStatusCode)
	}
	return response.ConnectionCallbacks
}

func managedTokenTestMessage(uid []byte, sequence uint64) *protobufs.AgentToServer {
	return &protobufs.AgentToServer{
		InstanceUid: uid,
		SequenceNum: sequence,
		AgentDescription: &protobufs.AgentDescription{
			IdentifyingAttributes: []*protobufs.KeyValue{{
				Key: "service.name",
				Value: &protobufs.AnyValue{
					Value: &protobufs.AnyValue_StringValue{StringValue: "otelcol-managed-test"},
				},
			}},
		},
	}
}

func TestOpAMPAuthRejectsMalformedAuthorizationWithGenericChallenge(t *testing.T) {
	_, _, generated := newManagedTokenServer(t, nil, Options{})

	tests := []struct {
		name    string
		headers []string
	}{
		{name: "missing"},
		{name: "empty", headers: []string{""}},
		{name: "basic", headers: []string{"Basic credentials"}},
		{name: "lowercase scheme", headers: []string{"bearer " + generated.Value}},
		{name: "leading whitespace", headers: []string{" Bearer " + generated.Value}},
		{name: "double space", headers: []string{"Bearer  " + generated.Value}},
		{name: "trailing whitespace", headers: []string{"Bearer " + generated.Value + " "}},
		{name: "tab separator", headers: []string{"Bearer\t" + generated.Value}},
		{name: "combined credentials", headers: []string{"Bearer " + generated.Value + ", Bearer " + generated.Value}},
		{name: "second scheme", headers: []string{"Bearer " + generated.Value + " Basic credentials"}},
		{name: "two fields", headers: []string{"Bearer " + generated.Value, "Bearer " + generated.Value}},
		{name: "malformed token", headers: []string{"Bearer ompt_not-a-token"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, store, _ := newManagedTokenServer(t, nil, Options{})
			req := httptest.NewRequest(http.MethodGet, "/v1/opamp", nil)
			for _, header := range tt.headers {
				req.Header.Add("Authorization", header)
			}
			assertGenericUnauthorized(t, server.authenticateRequest(req))
			if validate, _ := store.counts(); validate != 0 {
				t.Fatalf("malformed Authorization reached the token store %d times", validate)
			}
		})
	}
}

func TestOpAMPAuthRejectsAllInvalidManagedTokenStatesIdentically(t *testing.T) {
	tests := []struct {
		name     string
		value    func(opampauth.GeneratedToken) string
		storeErr error
	}{
		{
			name: "unknown ID",
			value: func(opampauth.GeneratedToken) string {
				other, err := opampauth.Generate()
				if err != nil {
					t.Fatalf("generate unknown token: %v", err)
				}
				return other.Value
			},
		},
		{
			name: "wrong hash",
			value: func(token opampauth.GeneratedToken) string {
				last := byte('A')
				if token.Value[len(token.Value)-1] == last {
					last = 'B'
				}
				return token.Value[:len(token.Value)-1] + string(last)
			},
		},
		{name: "expired", value: func(token opampauth.GeneratedToken) string { return token.Value }, storeErr: ext.ErrInvalidOpAMPToken},
		{name: "revoked", value: func(token opampauth.GeneratedToken) string { return token.Value }, storeErr: ext.ErrInvalidOpAMPToken},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, store, generated := newManagedTokenServer(t, nil, Options{})
			store.validateErr = tt.storeErr
			response := server.authenticateRequest(managedTokenRequest("Bearer " + tt.value(generated)))
			assertGenericUnauthorized(t, response)
		})
	}
}

func TestOpAMPAuthReturnsServiceUnavailableOnStoreFailure(t *testing.T) {
	server, store, generated := newManagedTokenServer(t, nil, Options{})
	store.validateErr = errors.New("database unavailable")

	response := server.authenticateRequest(managedTokenRequest("Bearer " + generated.Value))

	if response.Accept {
		t.Fatal("store failure accepted the connection")
	}
	if response.HTTPStatusCode != http.StatusServiceUnavailable {
		t.Fatalf("HTTP status = %d, want 503", response.HTTPStatusCode)
	}
	if response.HTTPResponseHeader["WWW-Authenticate"] != "" {
		t.Fatal("store failure exposed the invalid-credential challenge")
	}
}

func TestOpAMPAuthInvalidBurstDoesNotLockOutValidToken(t *testing.T) {
	server, _, generated := newManagedTokenServer(t, nil, Options{})
	for i := 0; i < 100; i++ {
		req := managedTokenRequest("Bearer ompt_invalid")
		req.RemoteAddr = "192.0.2.10:1234"
		assertGenericUnauthorized(t, server.authenticateRequest(req))
	}
	valid := managedTokenRequest("Bearer " + generated.Value)
	valid.RemoteAddr = "192.0.2.10:1234"
	if response := server.authenticateRequest(valid); !response.Accept {
		t.Fatalf("valid token rejected after invalid burst with HTTP status %d", response.HTTPStatusCode)
	}
}

func TestOpAMPAuthCapturesPrincipalInPerConnectionCallbacks(t *testing.T) {
	expiresAt := time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)
	now := expiresAt.Add(-time.Minute)
	clock := &tokenConnectionTestClock{now: now}
	timers := &tokenConnectionTestTimers{}
	server, _, generated := newManagedTokenServer(t, &expiresAt, Options{now: clock.Now, afterFunc: timers.AfterFunc})
	callbacks := authenticateManagedToken(t, server, generated.Value)
	conn := &recordingConn{}
	callbacks.OnConnected(context.Background(), conn)

	if got := server.DisconnectTokenConnections(generated.ID); got != 1 {
		t.Fatalf("DisconnectTokenConnections returned %d, want 1", got)
	}
	if conn.disconnects() != 1 {
		t.Fatalf("captured token principal disconnected %d times, want 1", conn.disconnects())
	}
}

func TestOpAMPAuthHandshakeWithoutFirstMessageDoesNotTouchActivity(t *testing.T) {
	server, store, generated := newManagedTokenServer(t, nil, Options{})
	callbacks := authenticateManagedToken(t, server, generated.Value)
	conn := &recordingConn{}
	callbacks.OnConnected(context.Background(), conn)
	callbacks.OnConnectionClose(conn)

	_, mark := store.counts()
	if mark != 0 {
		t.Fatalf("MarkOpAMPTokenUsed calls = %d, want 0", mark)
	}
}

func TestOpAMPAuthFirstMessageReservesUIDTouchesOnceThenMutatesWorkload(t *testing.T) {
	server, store, generated := newManagedTokenServer(t, nil, Options{})
	callbacks := authenticateManagedToken(t, server, generated.Value)
	conn := &recordingConn{}
	callbacks.OnConnected(context.Background(), conn)
	uid := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}

	if reply := callbacks.OnMessage(context.Background(), conn, managedTokenTestMessage(uid, 1)); reply != nil {
		t.Fatal("WebSocket-style connection returned a reply after sending it under the lease")
	}
	if reply := callbacks.OnMessage(context.Background(), conn, &protobufs.AgentToServer{InstanceUid: uid, SequenceNum: 2}); reply != nil {
		t.Fatal("subsequent WebSocket-style message returned a duplicate reply")
	}
	if got := conn.sentCount(); got != 2 {
		t.Fatalf("responses sent under lease = %d, want 2", got)
	}
	_, mark := store.counts()
	if mark != 1 {
		t.Fatalf("MarkOpAMPTokenUsed calls = %d, want 1", mark)
	}
	store.mu.Lock()
	upserts := len(store.upsertCalls)
	store.mu.Unlock()
	if upserts == 0 {
		t.Fatal("accepted first message did not mutate workload state")
	}
}

func TestOpAMPAuthRejectsInvalidInstanceUIDBeforeTouchOrMutation(t *testing.T) {
	server, store, generated := newManagedTokenServer(t, nil, Options{})
	callbacks := authenticateManagedToken(t, server, generated.Value)
	conn := &recordingConn{}
	callbacks.OnConnected(context.Background(), conn)

	if reply := callbacks.OnMessage(context.Background(), conn, managedTokenTestMessage([]byte("too-short"), 1)); reply != nil {
		t.Fatal("invalid instance UID received a response")
	}
	_, mark := store.counts()
	if mark != 0 {
		t.Fatalf("invalid UID touched token activity %d times", mark)
	}
	store.mu.Lock()
	upserts := len(store.upsertCalls)
	store.mu.Unlock()
	if upserts != 0 {
		t.Fatalf("invalid UID caused %d workload upserts", upserts)
	}
	if conn.disconnects() != 1 {
		t.Fatalf("invalid UID disconnected %d times, want 1", conn.disconnects())
	}
}

func TestOpAMPAuthConcurrentFirstMessagesTouchOnce(t *testing.T) {
	server, store, generated := newManagedTokenServer(t, nil, Options{})
	store.markStarted = make(chan struct{}, 1)
	store.markRelease = make(chan struct{})
	callbacks := authenticateManagedToken(t, server, generated.Value)
	conn := &recordingConn{}
	callbacks.OnConnected(context.Background(), conn)
	uid := []byte{0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
		0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		callbacks.OnMessage(context.Background(), conn, managedTokenTestMessage(uid, 1))
	}()
	select {
	case <-store.markStarted:
	case <-time.After(time.Second):
		t.Fatal("first message did not reach MarkOpAMPTokenUsed")
	}
	go func() {
		defer wg.Done()
		callbacks.OnMessage(context.Background(), conn, managedTokenTestMessage(uid, 2))
	}()
	close(store.markRelease)
	wg.Wait()

	_, mark := store.counts()
	if mark != 1 {
		t.Fatalf("concurrent first messages touched activity %d times, want 1", mark)
	}
}

func TestOpAMPAuthConcurrentFirstMessagesCannotRetryFailedAdmission(t *testing.T) {
	server, store, generated := newManagedTokenServer(t, nil, Options{})
	store.markErr = errors.New("database unavailable")
	store.markStarted = make(chan struct{}, 1)
	store.markRelease = make(chan struct{})
	callbacks := authenticateManagedToken(t, server, generated.Value)
	conn := &recordingConn{}
	callbacks.OnConnected(context.Background(), conn)
	uid := []byte{0xa1, 0xa2, 0xa3, 0xa4, 0xa5, 0xa6, 0xa7, 0xa8,
		0xa9, 0xaa, 0xab, 0xac, 0xad, 0xae, 0xaf, 0xb0}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		callbacks.OnMessage(context.Background(), conn, managedTokenTestMessage(uid, 1))
	}()
	select {
	case <-store.markStarted:
	case <-time.After(time.Second):
		t.Fatal("first message did not reach MarkOpAMPTokenUsed")
	}
	go func() {
		defer wg.Done()
		callbacks.OnMessage(context.Background(), conn, managedTokenTestMessage(uid, 2))
	}()
	close(store.markRelease)
	wg.Wait()

	_, mark := store.counts()
	store.mu.Lock()
	upserts := len(store.upsertCalls)
	store.mu.Unlock()
	if mark != 1 || upserts != 0 {
		t.Fatalf("terminal admission failure allowed mark=%d upserts=%d, want 1 and 0", mark, upserts)
	}
}

func TestOpAMPAuthRequiresExactlySixteenUIDBytesAndRejectsChanges(t *testing.T) {
	invalidLengths := []int{0, 15, 17}
	for _, length := range invalidLengths {
		t.Run(fmt.Sprintf("length_%d", length), func(t *testing.T) {
			server, store, generated := newManagedTokenServer(t, nil, Options{})
			callbacks := authenticateManagedToken(t, server, generated.Value)
			conn := &recordingConn{}
			callbacks.OnConnected(context.Background(), conn)
			callbacks.OnMessage(context.Background(), conn, managedTokenTestMessage(make([]byte, length), 1))
			_, mark := store.counts()
			if mark != 0 || conn.disconnects() != 1 {
				t.Fatalf("UID length %d caused mark=%d disconnects=%d", length, mark, conn.disconnects())
			}
		})
	}

	server, store, generated := newManagedTokenServer(t, nil, Options{})
	callbacks := authenticateManagedToken(t, server, generated.Value)
	conn := &recordingConn{}
	callbacks.OnConnected(context.Background(), conn)
	firstUID := make([]byte, 16)
	secondUID := make([]byte, 16)
	secondUID[15] = 1
	callbacks.OnMessage(context.Background(), conn, managedTokenTestMessage(firstUID, 1))
	callbacks.OnMessage(context.Background(), conn, managedTokenTestMessage(secondUID, 2))
	_, mark := store.counts()
	if mark != 1 || conn.disconnects() != 1 {
		t.Fatalf("UID change caused mark=%d disconnects=%d, want 1 and 1", mark, conn.disconnects())
	}
}

func TestOpAMPAuthHotExpiryRejectsMessagesAndPushesWithDelayedTimer(t *testing.T) {
	now := time.Date(2026, 7, 22, 9, 0, 0, 0, time.UTC)
	expiresAt := now.Add(time.Minute)
	clock := &tokenConnectionTestClock{now: now}
	timers := &tokenConnectionTestTimers{}
	server, store, generated := newManagedTokenServer(t, &expiresAt, Options{now: clock.Now, afterFunc: timers.AfterFunc})
	callbacks := authenticateManagedToken(t, server, generated.Value)
	conn := &recordingConn{disconnectErr: errors.New("close failed")}
	callbacks.OnConnected(context.Background(), conn)
	uid := []byte{0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28,
		0x29, 0x2a, 0x2b, 0x2c, 0x2d, 0x2e, 0x2f, 0x30}
	callbacks.OnMessage(context.Background(), conn, managedTokenTestMessage(uid, 1))
	_, markBefore := store.counts()
	store.mu.Lock()
	upsertsBefore := len(store.upsertCalls)
	store.mu.Unlock()

	// Do not fire the manual timer: synchronous Acquire must enforce the exact deadline.
	clock.Set(expiresAt)
	if reply := callbacks.OnMessage(context.Background(), conn, managedTokenTestMessage(uid, 2)); reply != nil {
		t.Fatal("message at expires_at received a response")
	}
	if err := server.PushConfig(context.Background(), "ignored", []byte("receivers: {}"), "2122232425262728292a2b2c2d2e2f30"); err == nil {
		t.Fatal("push at expires_at succeeded")
	}
	_, markAfter := store.counts()
	store.mu.Lock()
	upsertsAfter := len(store.upsertCalls)
	store.mu.Unlock()
	if markAfter != markBefore || upsertsAfter != upsertsBefore {
		t.Fatalf("expired operation mutated state: marks %d->%d, upserts %d->%d", markBefore, markAfter, upsertsBefore, upsertsAfter)
	}
	if conn.disconnects() == 0 {
		t.Fatal("expired connection was not disconnected")
	}
	if server.GetConnection("2122232425262728292a2b2c2d2e2f30") != nil {
		t.Fatal("expiry Disconnect error left the UID owned")
	}
}

func TestDisconnectTokenConnectionsWaitsForAdmittedMessageAndIsolatesToken(t *testing.T) {
	server, store, generated := newManagedTokenServer(t, nil, Options{})
	store.markStarted = make(chan struct{}, 1)
	store.markRelease = make(chan struct{})
	callbacks := authenticateManagedToken(t, server, generated.Value)
	conn := &recordingConn{}
	callbacks.OnConnected(context.Background(), conn)
	uid := []byte{0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x38,
		0x39, 0x3a, 0x3b, 0x3c, 0x3d, 0x3e, 0x3f, 0x40}
	messageDone := make(chan struct{})
	go func() {
		callbacks.OnMessage(context.Background(), conn, managedTokenTestMessage(uid, 1))
		close(messageDone)
	}()
	select {
	case <-store.markStarted:
	case <-time.After(time.Second):
		t.Fatal("message did not reach MarkOpAMPTokenUsed")
	}

	disconnected := make(chan int, 1)
	go func() { disconnected <- server.DisconnectTokenConnections(generated.ID) }()
	waitForTokenDisableState(t, server.tokens, generated.ID)
	select {
	case <-disconnected:
		t.Fatal("DisconnectTokenConnections returned before admitted message completed")
	case <-time.After(30 * time.Millisecond):
	}
	close(store.markRelease)
	select {
	case <-messageDone:
	case <-time.After(time.Second):
		t.Fatal("message did not complete")
	}
	select {
	case count := <-disconnected:
		if count != 1 {
			t.Fatalf("DisconnectTokenConnections returned %d, want 1", count)
		}
	case <-time.After(time.Second):
		t.Fatal("DisconnectTokenConnections did not return")
	}
}

func TestDisconnectTokenConnectionsWaitsForWebSocketReplySend(t *testing.T) {
	server, _, generated := newManagedTokenServer(t, nil, Options{})
	callbacks := authenticateManagedToken(t, server, generated.Value)
	conn := &recordingConn{sendStarted: make(chan struct{}, 1), sendRelease: make(chan struct{})}
	callbacks.OnConnected(context.Background(), conn)
	uid := []byte{0xd1, 0xd2, 0xd3, 0xd4, 0xd5, 0xd6, 0xd7, 0xd8,
		0xd9, 0xda, 0xdb, 0xdc, 0xdd, 0xde, 0xdf, 0xe0}
	messageDone := make(chan struct{})
	go func() {
		callbacks.OnMessage(context.Background(), conn, managedTokenTestMessage(uid, 1))
		close(messageDone)
	}()
	select {
	case <-conn.sendStarted:
	case <-time.After(time.Second):
		t.Fatal("OnMessage did not send its WebSocket reply under the lease")
	}

	disconnected := make(chan int, 1)
	go func() { disconnected <- server.DisconnectTokenConnections(generated.ID) }()
	waitForTokenDisableState(t, server.tokens, generated.ID)
	select {
	case <-disconnected:
		t.Fatal("DisconnectTokenConnections returned before the WebSocket reply send completed")
	case <-time.After(30 * time.Millisecond):
	}
	close(conn.sendRelease)
	select {
	case <-messageDone:
	case <-time.After(time.Second):
		t.Fatal("OnMessage did not finish after reply send")
	}
	select {
	case <-disconnected:
	case <-time.After(time.Second):
		t.Fatal("DisconnectTokenConnections did not finish after reply send")
	}
}

func TestRevocationRacingSendErrorDisconnectsTransportOnce(t *testing.T) {
	server, _, generated := newManagedTokenServer(t, nil, Options{})
	callbacks := authenticateManagedToken(t, server, generated.Value)
	conn := &recordingConn{}
	callbacks.OnConnected(context.Background(), conn)
	uid := []byte{0xd1, 0xd2, 0xd3, 0xd4, 0xd5, 0xd6, 0xd7, 0xd8,
		0xd9, 0xda, 0xdb, 0xdc, 0xdd, 0xde, 0xdf, 0xe0}
	callbacks.OnMessage(context.Background(), conn, managedTokenTestMessage(uid, 1))
	conn.sendStarted = make(chan struct{}, 1)
	conn.sendRelease = make(chan struct{})
	conn.sendErr = errors.New("write failed")

	messageDone := make(chan struct{})
	go func() {
		callbacks.OnMessage(context.Background(), conn, managedTokenTestMessage(uid, 2))
		close(messageDone)
	}()
	select {
	case <-conn.sendStarted:
	case <-time.After(time.Second):
		t.Fatal("message reply did not reach Connection.Send")
	}
	disableDone := make(chan int, 1)
	go func() { disableDone <- server.DisconnectTokenConnections(generated.ID) }()
	waitForTokenDisableState(t, server.tokens, generated.ID)
	close(conn.sendRelease)
	select {
	case <-messageDone:
	case <-time.After(time.Second):
		t.Fatal("message did not finish after the Send error")
	}
	select {
	case count := <-disableDone:
		if count != 1 {
			t.Fatalf("revocation disconnected %d sessions, want 1", count)
		}
	case <-time.After(time.Second):
		t.Fatal("revocation did not finish after the Send error")
	}
	if conn.disconnects() != 1 {
		t.Fatalf("racing Send error and revocation disconnected %d times, want 1", conn.disconnects())
	}
}

func TestOpAMPAuthHTTPMessageReturnsReplyWhenDirectSendIsUnsupported(t *testing.T) {
	server, store, generated := newManagedTokenServer(t, nil, Options{})
	callbacks := authenticateManagedToken(t, server, generated.Value)
	conn := &recordingConn{sendErr: opampServer.ErrInvalidHTTPConnection}
	callbacks.OnConnected(context.Background(), conn)
	defer callbacks.OnConnectionClose(conn)
	uid := []byte{0xe1, 0xe2, 0xe3, 0xe4, 0xe5, 0xe6, 0xe7, 0xe8,
		0xe9, 0xea, 0xeb, 0xec, 0xed, 0xee, 0xef, 0xf0}

	reply := callbacks.OnMessage(context.Background(), conn, managedTokenTestMessage(uid, 1))
	if reply == nil {
		t.Fatal("HTTP-style OpAMP connection did not return its handler-written reply")
	}
	_, mark := store.counts()
	if mark != 1 {
		t.Fatalf("HTTP-style first message touched activity %d times, want 1", mark)
	}
}

func TestPlainHTTPDisconnectWaitsForResponseWriteAndClearsDeadlineOnClose(t *testing.T) {
	server, _, generated := newManagedTokenServer(t, nil, Options{writeTimeout: time.Second})
	handler, connContext, err := server.Attach()
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}

	requestBody, err := proto.Marshal(managedTokenTestMessage([]byte{
		0x81, 0x82, 0x83, 0x84, 0x85, 0x86, 0x87, 0x88,
		0x89, 0x8a, 0x8b, 0x8c, 0x8d, 0x8e, 0x8f, 0x90,
	}, 1))
	if err != nil {
		t.Fatalf("marshal plain HTTP request: %v", err)
	}
	serverConn, peerConn := net.Pipe()
	defer peerConn.Close()
	deadlineConn := &writeDeadlineConn{Conn: serverConn}
	defer deadlineConn.Close()
	req := httptest.NewRequest(http.MethodPost, "/v1/opamp", bytes.NewReader(requestBody))
	req.Header.Set("Authorization", "Bearer "+generated.Value)
	req.Header.Set("Content-Type", "application/x-protobuf")
	req = req.WithContext(connContext(req.Context(), deadlineConn))
	response := newBlockingResponseWriter()

	handlerDone := make(chan struct{})
	go func() {
		handler(response, req)
		close(handlerDone)
	}()
	select {
	case <-response.writeStarted:
	case <-time.After(time.Second):
		t.Fatal("plain HTTP handler did not reach ResponseWriter.Write")
	}
	deadlines := deadlineConn.deadlineSnapshot()
	if len(deadlines) == 0 || deadlines[len(deadlines)-1].IsZero() {
		close(response.writeRelease)
		t.Fatal("plain HTTP response write had no active socket deadline")
	}

	disabled := make(chan int, 1)
	go func() { disabled <- server.DisconnectTokenConnections(generated.ID) }()
	waitForTokenDisableState(t, server.tokens, generated.ID)
	select {
	case <-disabled:
		close(response.writeRelease)
		t.Fatal("DisconnectTokenConnections returned before the plain HTTP response write")
	case <-time.After(30 * time.Millisecond):
	}

	close(response.writeRelease)
	select {
	case <-handlerDone:
	case <-time.After(time.Second):
		t.Fatal("plain HTTP handler did not finish after the response write")
	}
	select {
	case count := <-disabled:
		if count != 1 {
			t.Fatalf("DisconnectTokenConnections returned %d, want 1", count)
		}
	case <-time.After(time.Second):
		t.Fatal("DisconnectTokenConnections did not finish after the response write")
	}
	deadlines = deadlineConn.deadlineSnapshot()
	if len(deadlines) < 2 || !deadlines[len(deadlines)-1].IsZero() {
		t.Fatalf("plain HTTP write deadline was not cleared on close: %v", deadlines)
	}
}

func TestPlainHTTPResponseLeaseCoversBufferedFlush(t *testing.T) {
	server, _, generated := newManagedTokenServer(t, nil, Options{writeTimeout: time.Second})
	handler, connContext, err := server.Attach()
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	requestBody, err := proto.Marshal(managedTokenTestMessage([]byte{
		0x01, 0x12, 0x23, 0x34, 0x45, 0x56, 0x67, 0x78,
		0x89, 0x9a, 0xab, 0xbc, 0xcd, 0xde, 0xef, 0xf0,
	}, 1))
	if err != nil {
		t.Fatalf("marshal plain HTTP request: %v", err)
	}
	serverConn, peerConn := net.Pipe()
	defer peerConn.Close()
	deadlineConn := &writeDeadlineConn{Conn: serverConn}
	defer deadlineConn.Close()
	req := httptest.NewRequest(http.MethodPost, "/v1/opamp", bytes.NewReader(requestBody))
	req.Header.Set("Authorization", "Bearer "+generated.Value)
	req.Header.Set("Content-Type", "application/x-protobuf")
	req = req.WithContext(connContext(req.Context(), deadlineConn))
	response := newBlockingFlushResponseWriter()

	handlerDone := make(chan struct{})
	go func() {
		handler(response, req)
		close(handlerDone)
	}()
	select {
	case <-response.writeStarted:
	case <-time.After(time.Second):
		t.Fatal("plain HTTP handler did not buffer its response")
	}
	select {
	case <-response.flushStarted:
	case <-time.After(time.Second):
		close(response.flushRelease)
		t.Fatal("plain HTTP response was not flushed before OnConnectionClose")
	}
	deadlines := deadlineConn.deadlineSnapshot()
	if len(deadlines) == 0 || deadlines[len(deadlines)-1].IsZero() {
		close(response.flushRelease)
		t.Fatal("plain HTTP flush had no active socket deadline")
	}

	disabled := make(chan int, 1)
	go func() { disabled <- server.DisconnectTokenConnections(generated.ID) }()
	waitForTokenDisableState(t, server.tokens, generated.ID)
	select {
	case <-disabled:
		close(response.flushRelease)
		t.Fatal("DisconnectTokenConnections returned before the buffered response flush")
	case <-time.After(30 * time.Millisecond):
	}
	select {
	case <-handlerDone:
		close(response.flushRelease)
		t.Fatal("plain HTTP handler returned before the buffered response flush")
	default:
	}

	close(response.flushRelease)
	select {
	case <-handlerDone:
	case <-time.After(time.Second):
		t.Fatal("plain HTTP handler did not finish after the response flush")
	}
	select {
	case count := <-disabled:
		if count != 1 {
			t.Fatalf("DisconnectTokenConnections returned %d, want 1", count)
		}
	case <-time.After(time.Second):
		t.Fatal("DisconnectTokenConnections did not finish after the response flush")
	}
	deadlines = deadlineConn.deadlineSnapshot()
	if len(deadlines) < 2 || !deadlines[len(deadlines)-1].IsZero() {
		t.Fatalf("plain HTTP write deadline was not cleared after flush: %v", deadlines)
	}
}

func TestWebSocketSendUsesEarlierContextDeadlineAndClearsIt(t *testing.T) {
	server, _, generated := newManagedTokenServer(t, nil, Options{writeTimeout: time.Second})
	callbacks := authenticateManagedToken(t, server, generated.Value)
	serverConn, peerConn := net.Pipe()
	defer serverConn.Close()
	defer peerConn.Close()
	deadlineConn := &writeDeadlineConn{Conn: serverConn}
	conn := &recordingConn{netConn: deadlineConn}
	callbacks.OnConnected(context.Background(), conn)
	uid := []byte{0x91, 0x92, 0x93, 0x94, 0x95, 0x96, 0x97, 0x98,
		0x99, 0x9a, 0x9b, 0x9c, 0x9d, 0x9e, 0x9f, 0xa0}
	callbacks.OnMessage(context.Background(), conn, managedTokenTestMessage(uid, 1))
	deadlineConn.resetDeadlines()
	workloadID, ok := server.registry.LookupWorkload(hex.EncodeToString(uid))
	if !ok {
		t.Fatal("accepted instance has no workload binding")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	contextDeadline, _ := ctx.Deadline()
	if err := server.PushConfig(ctx, workloadID, []byte("receivers: {}"), hex.EncodeToString(uid)); err != nil {
		t.Fatalf("PushConfig: %v", err)
	}
	deadlines := deadlineConn.deadlineSnapshot()
	if len(deadlines) != 2 {
		t.Fatalf("SetWriteDeadline calls = %d, want set then clear", len(deadlines))
	}
	if deadlines[0].After(contextDeadline) {
		t.Fatalf("write deadline %s exceeds context deadline %s", deadlines[0], contextDeadline)
	}
	if !deadlines[1].IsZero() {
		t.Fatalf("WebSocket write deadline was not cleared: %s", deadlines[1])
	}
}

func TestSessionSendContextDeadlineBoundsBlockedSocketWrite(t *testing.T) {
	serverConn, peerConn := net.Pipe()
	defer peerConn.Close()
	deadlineConn := &writeDeadlineConn{Conn: serverConn}
	conn := newPipeWritingConn(deadlineConn)
	defer conn.Disconnect()
	session := &tokenSession{conn: conn}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	startedAt := time.Now()
	err := session.send(ctx, &protobufs.ServerToAgent{}, 2*time.Second)
	elapsed := time.Since(startedAt)
	if err == nil {
		t.Fatal("blocked socket write succeeded")
	}
	if elapsed < 20*time.Millisecond {
		t.Fatalf("blocked socket write returned too early after %s", elapsed)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("blocked socket write ignored the earlier context deadline: %s", elapsed)
	}
	deadlines := deadlineConn.deadlineSnapshot()
	if len(deadlines) < 2 || !deadlines[len(deadlines)-1].IsZero() {
		t.Fatalf("blocked socket write deadline was not cleared: %v", deadlines)
	}
}

func TestPlainHTTPConcurrentPushClearsDeadlineAfterConnectionClose(t *testing.T) {
	server, _, generated := newManagedTokenServer(t, nil, Options{writeTimeout: time.Second})
	callbacks := authenticateManagedToken(t, server, generated.Value)
	serverConn, peerConn := net.Pipe()
	defer peerConn.Close()
	deadlineConn := &writeDeadlineConn{Conn: serverConn}
	defer deadlineConn.Close()
	conn := &recordingConn{
		netConn: deadlineConn,
		sendErr: opampServer.ErrInvalidHTTPConnection,
	}
	callbacks.OnConnected(context.Background(), conn)
	uid := []byte{0x10, 0x21, 0x32, 0x43, 0x54, 0x65, 0x76, 0x87,
		0x98, 0xa9, 0xba, 0xcb, 0xdc, 0xed, 0xfe, 0x0f}
	uidHex := hex.EncodeToString(uid)
	if reply := callbacks.OnMessage(context.Background(), conn, managedTokenTestMessage(uid, 1)); reply == nil {
		t.Fatal("plain HTTP message did not retain its handler-written response")
	}
	workloadID, ok := server.registry.LookupWorkload(uidHex)
	if !ok {
		t.Fatal("plain HTTP session has no workload binding")
	}
	server.mu.RLock()
	session := server.conns[uidHex]
	server.mu.RUnlock()
	if session == nil {
		t.Fatal("plain HTTP session was not registered")
	}

	session.sendGate.Lock()
	gateLocked := true
	defer func() {
		if gateLocked {
			session.sendGate.Unlock()
		}
	}()
	pushDone := make(chan error, 1)
	go func() {
		pushDone <- server.PushConfig(context.Background(), workloadID, []byte("receivers: {}"), uidHex)
	}()
	leaseDeadline := time.NewTimer(time.Second)
	defer leaseDeadline.Stop()
	leaseTicker := time.NewTicker(time.Millisecond)
	defer leaseTicker.Stop()
	for session.leases.Load() != 2 {
		select {
		case <-leaseTicker.C:
		case <-leaseDeadline.C:
			t.Fatal("concurrent push did not acquire its session lease")
		}
	}

	callbacks.OnConnectionClose(conn)
	deadlines := deadlineConn.deadlineSnapshot()
	if len(deadlines) < 2 || !deadlines[len(deadlines)-1].IsZero() {
		t.Fatalf("OnConnectionClose did not clear the response deadline: %v", deadlines)
	}
	session.sendGate.Unlock()
	gateLocked = false
	select {
	case err := <-pushDone:
		if !errors.Is(err, opampServer.ErrInvalidHTTPConnection) {
			t.Fatalf("concurrent plain HTTP push error = %v, want ErrInvalidHTTPConnection", err)
		}
	case <-time.After(time.Second):
		t.Fatal("concurrent plain HTTP push did not finish")
	}
	deadlines = deadlineConn.deadlineSnapshot()
	if !deadlines[len(deadlines)-1].IsZero() {
		t.Fatalf("concurrent push left a stale write deadline after close: %v", deadlines)
	}
}

func TestSessionSendClosesTransportWhenDeadlineClearFailsAfterSendError(t *testing.T) {
	serverConn, peerConn := net.Pipe()
	defer peerConn.Close()
	deadlineConn := &writeDeadlineConn{Conn: serverConn, closed: make(chan struct{})}
	deadlineConn.injectDeadlineErrors(nil, errors.New("clear deadline failed"))
	conn := &recordingConn{
		netConn: deadlineConn,
		sendErr: opampServer.ErrInvalidHTTPConnection,
	}
	session := &tokenSession{conn: conn}

	err := session.send(context.Background(), &protobufs.ServerToAgent{}, time.Second)
	if !errors.Is(err, opampServer.ErrInvalidHTTPConnection) {
		t.Fatalf("Send error = %v, want ErrInvalidHTTPConnection", err)
	}
	select {
	case <-deadlineConn.closed:
	case <-time.After(time.Second):
		t.Fatal("deadline clear failure after Send error did not close the transport")
	}
}

func TestPushConfigDeadlineFailureDisconnectsSession(t *testing.T) {
	tests := []struct {
		name     string
		setErr   error
		clearErr error
	}{
		{name: "set", setErr: errors.New("set deadline failed")},
		{name: "clear", clearErr: errors.New("clear deadline failed")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, _, generated := newManagedTokenServer(t, nil, Options{writeTimeout: time.Second})
			callbacks := authenticateManagedToken(t, server, generated.Value)
			serverConn, peerConn := net.Pipe()
			defer serverConn.Close()
			defer peerConn.Close()
			deadlineConn := &writeDeadlineConn{Conn: serverConn}
			conn := &recordingConn{netConn: deadlineConn}
			callbacks.OnConnected(context.Background(), conn)
			uid := []byte{0xb1, 0xb2, 0xb3, 0xb4, 0xb5, 0xb6, 0xb7, 0xb8,
				0xb9, 0xba, 0xbb, 0xbc, 0xbd, 0xbe, 0xbf, 0xc0}
			uidHex := hex.EncodeToString(uid)
			callbacks.OnMessage(context.Background(), conn, managedTokenTestMessage(uid, 1))
			workloadID, ok := server.registry.LookupWorkload(uidHex)
			if !ok {
				t.Fatal("accepted instance has no workload binding")
			}
			deadlineConn.injectDeadlineErrors(tt.setErr, tt.clearErr)

			if err := server.PushConfig(context.Background(), workloadID, []byte("receivers: {}"), uidHex); err == nil {
				t.Fatal("PushConfig succeeded despite the injected deadline error")
			}
			if conn.disconnects() != 1 {
				t.Fatalf("deadline error disconnected transport %d times, want 1", conn.disconnects())
			}
			if server.GetConnection(uidHex) != nil {
				t.Fatal("deadline error left the session active")
			}
		})
	}
}

func TestPlainHTTPClearDeadlineFailureClosesTransport(t *testing.T) {
	server, _, generated := newManagedTokenServer(t, nil, Options{writeTimeout: time.Second})
	handler, connContext, err := server.Attach()
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	requestBody, err := proto.Marshal(managedTokenTestMessage([]byte{
		0xc1, 0xc2, 0xc3, 0xc4, 0xc5, 0xc6, 0xc7, 0xc8,
		0xc9, 0xca, 0xcb, 0xcc, 0xcd, 0xce, 0xcf, 0xd0,
	}, 1))
	if err != nil {
		t.Fatalf("marshal plain HTTP request: %v", err)
	}
	serverConn, peerConn := net.Pipe()
	defer peerConn.Close()
	deadlineConn := &writeDeadlineConn{Conn: serverConn, closed: make(chan struct{})}
	defer deadlineConn.Close()
	deadlineConn.injectDeadlineErrors(nil, errors.New("clear deadline failed"))
	req := httptest.NewRequest(http.MethodPost, "/v1/opamp", bytes.NewReader(requestBody))
	req.Header.Set("Authorization", "Bearer "+generated.Value)
	req.Header.Set("Content-Type", "application/x-protobuf")
	req = req.WithContext(connContext(req.Context(), deadlineConn))

	handler(httptest.NewRecorder(), req)
	select {
	case <-deadlineConn.closed:
	default:
		t.Fatal("plain HTTP clear deadline error did not close the transport")
	}
}

func TestBlockedSocketSendIsBoundedForRevocationAndExpiry(t *testing.T) {
	tests := []struct {
		name       string
		expires    bool
		disableNow bool
	}{
		{name: "revocation", disableNow: true},
		{name: "expiry", expires: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var expiresAt *time.Time
			opts := Options{writeTimeout: 80 * time.Millisecond}
			var clock *tokenConnectionTestClock
			var timers *tokenConnectionTestTimers
			if tt.expires {
				now := time.Date(2026, 7, 22, 9, 0, 0, 0, time.UTC)
				expires := now.Add(time.Minute)
				expiresAt = &expires
				clock = &tokenConnectionTestClock{now: now}
				timers = &tokenConnectionTestTimers{}
				opts.now = clock.Now
				opts.afterFunc = timers.AfterFunc
			}
			server, _, generated := newManagedTokenServer(t, expiresAt, opts)
			callbacks := authenticateManagedToken(t, server, generated.Value)
			serverConn, peerConn := net.Pipe()
			defer peerConn.Close()
			conn := newPipeWritingConn(serverConn)
			defer conn.Disconnect()
			callbacks.OnConnected(context.Background(), conn)
			uid := []byte{0xa1, 0xa2, 0xa3, 0xa4, 0xa5, 0xa6, 0xa7, 0xa8,
				0xa9, 0xaa, 0xab, 0xac, 0xad, 0xae, 0xaf, 0xb0}
			messageDone := make(chan struct{})
			go func() {
				callbacks.OnMessage(context.Background(), conn, managedTokenTestMessage(uid, 1))
				close(messageDone)
			}()
			select {
			case <-conn.sendStarted:
			case <-time.After(time.Second):
				t.Fatal("message reply did not enter the blocked socket write")
			}

			disableDone := make(chan int, 1)
			if tt.disableNow {
				go func() { disableDone <- server.DisconnectTokenConnections(generated.ID) }()
			} else {
				clock.Set(*expiresAt)
				_, scheduled := timers.Snapshot()
				if len(scheduled) != 1 {
					t.Fatalf("expiry timers = %d, want 1", len(scheduled))
				}
				go scheduled[0].Fire()
			}
			select {
			case <-messageDone:
			case <-time.After(500 * time.Millisecond):
				_ = peerConn.Close()
				t.Fatal("blocked socket Send outlived the configured write timeout")
			}
			if tt.disableNow {
				select {
				case count := <-disableDone:
					if count != 1 {
						t.Fatalf("DisconnectTokenConnections returned %d, want 1", count)
					}
				case <-time.After(time.Second):
					t.Fatal("revocation did not finish after the write deadline")
				}
			} else {
				select {
				case <-conn.disconnected:
				case <-time.After(time.Second):
					t.Fatal("expiry did not disconnect the blocked session")
				}
			}
			if got := server.GetConnection(hex.EncodeToString(uid)); got != nil {
				t.Fatal("bounded send left the session active")
			}
		})
	}
}

func TestSessionSerializesConcurrentReplyAndPushSends(t *testing.T) {
	server, _, generated := newManagedTokenServer(t, nil, Options{})
	callbacks := authenticateManagedToken(t, server, generated.Value)
	conn := &recordingConn{}
	callbacks.OnConnected(context.Background(), conn)
	uid := []byte{0xf1, 0xf2, 0xf3, 0xf4, 0xf5, 0xf6, 0xf7, 0xf8,
		0xf9, 0xfa, 0xfb, 0xfc, 0xfd, 0xfe, 0xff, 0x00}
	uidHex := hex.EncodeToString(uid)
	callbacks.OnMessage(context.Background(), conn, managedTokenTestMessage(uid, 1))
	workloadID, ok := server.registry.LookupWorkload(uidHex)
	if !ok {
		t.Fatal("accepted instance has no workload binding")
	}

	conn.sendStarted = make(chan struct{}, 2)
	conn.sendRelease = make(chan struct{})
	messageDone := make(chan struct{})
	go func() {
		callbacks.OnMessage(context.Background(), conn, managedTokenTestMessage(uid, 2))
		close(messageDone)
	}()
	select {
	case <-conn.sendStarted:
	case <-time.After(time.Second):
		t.Fatal("message reply did not enter Connection.Send")
	}

	pushDone := make(chan error, 1)
	go func() {
		pushDone <- server.PushConfig(context.Background(), workloadID, []byte("receivers: {}"), uidHex)
	}()
	select {
	case <-conn.sendStarted:
		close(conn.sendRelease)
		t.Fatal("push entered Connection.Send while the message reply send was active")
	case <-time.After(30 * time.Millisecond):
	}

	close(conn.sendRelease)
	select {
	case <-messageDone:
	case <-time.After(time.Second):
		t.Fatal("message reply did not complete after releasing the send barrier")
	}
	select {
	case err := <-pushDone:
		if err != nil {
			t.Fatalf("serialized push failed: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("push did not complete after the message reply send")
	}
	if got := conn.maxConcurrentSends(); got != 1 {
		t.Fatalf("maximum concurrent Connection.Send calls = %d, want 1", got)
	}
}

func TestDisconnectTokenConnectionsWaitsForAdmittedPush(t *testing.T) {
	server, _, generated := newManagedTokenServer(t, nil, Options{})
	callbacks := authenticateManagedToken(t, server, generated.Value)
	conn := &recordingConn{}
	callbacks.OnConnected(context.Background(), conn)
	uid := []byte{0x41, 0x42, 0x43, 0x44, 0x45, 0x46, 0x47, 0x48,
		0x49, 0x4a, 0x4b, 0x4c, 0x4d, 0x4e, 0x4f, 0x50}
	callbacks.OnMessage(context.Background(), conn, managedTokenTestMessage(uid, 1))
	conn.sendStarted = make(chan struct{}, 1)
	conn.sendRelease = make(chan struct{})
	workloadID, ok := server.registry.LookupWorkload("4142434445464748494a4b4c4d4e4f50")
	if !ok {
		t.Fatal("accepted instance has no workload binding")
	}

	pushDone := make(chan error, 1)
	go func() {
		pushDone <- server.PushConfig(context.Background(), workloadID, []byte("receivers: {}"), "4142434445464748494a4b4c4d4e4f50")
	}()
	select {
	case <-conn.sendStarted:
	case <-time.After(time.Second):
		t.Fatal("push did not reach Connection.Send")
	}
	disconnected := make(chan int, 1)
	go func() { disconnected <- server.DisconnectTokenConnections(generated.ID) }()
	waitForTokenDisableState(t, server.tokens, generated.ID)
	select {
	case <-disconnected:
		t.Fatal("DisconnectTokenConnections returned before admitted push completed")
	case <-time.After(30 * time.Millisecond):
	}
	close(conn.sendRelease)
	if err := <-pushDone; err != nil {
		t.Fatalf("admitted push failed: %v", err)
	}
	select {
	case <-disconnected:
	case <-time.After(time.Second):
		t.Fatal("DisconnectTokenConnections did not return after push")
	}
}

func TestDisconnectTokenWaitsForPushRemovedByConcurrentClose(t *testing.T) {
	server, _, generated := newManagedTokenServer(t, nil, Options{})
	callbacks := authenticateManagedToken(t, server, generated.Value)
	conn := &recordingConn{}
	callbacks.OnConnected(context.Background(), conn)
	uid := []byte{0xb1, 0xb2, 0xb3, 0xb4, 0xb5, 0xb6, 0xb7, 0xb8,
		0xb9, 0xba, 0xbb, 0xbc, 0xbd, 0xbe, 0xbf, 0xc0}
	uidHex := "b1b2b3b4b5b6b7b8b9babbbcbdbebfc0"
	callbacks.OnMessage(context.Background(), conn, managedTokenTestMessage(uid, 1))
	workloadID, ok := server.registry.LookupWorkload(uidHex)
	if !ok {
		t.Fatal("accepted instance has no workload binding")
	}
	conn.sendStarted = make(chan struct{}, 1)
	conn.sendRelease = make(chan struct{})

	pushDone := make(chan error, 1)
	go func() {
		pushDone <- server.PushConfig(context.Background(), workloadID, []byte("receivers: {}"), uidHex)
	}()
	select {
	case <-conn.sendStarted:
	case <-time.After(time.Second):
		t.Fatal("push did not reach Connection.Send")
	}

	closeDone := make(chan struct{})
	go func() {
		callbacks.OnConnectionClose(conn)
		close(closeDone)
	}()
	select {
	case <-closeDone:
	case <-time.After(time.Second):
		t.Fatal("OnConnectionClose blocked behind the active Send lease")
	}

	disabled := make(chan int, 1)
	go func() { disabled <- server.DisconnectTokenConnections(generated.ID) }()
	waitForTokenDisableState(t, server.tokens, generated.ID)
	select {
	case <-disabled:
		t.Fatal("Disable returned while the removed session still held a Send lease")
	case <-time.After(30 * time.Millisecond):
	}
	close(conn.sendRelease)
	if err := <-pushDone; err != nil {
		t.Fatalf("admitted push failed: %v", err)
	}
	select {
	case count := <-disabled:
		if count != 1 {
			t.Fatalf("Disable returned %d draining sessions, want 1", count)
		}
	case <-time.After(time.Second):
		t.Fatal("Disable did not finish after the removed Send lease")
	}
}

func TestDisconnectTokenAllowsReentrantCloseCallback(t *testing.T) {
	server, _, generated := newManagedTokenServer(t, nil, Options{})
	callbacks := authenticateManagedToken(t, server, generated.Value)
	conn := &recordingConn{}
	conn.disconnectHook = func() { callbacks.OnConnectionClose(conn) }
	callbacks.OnConnected(context.Background(), conn)
	uid := []byte{0xc1, 0xc2, 0xc3, 0xc4, 0xc5, 0xc6, 0xc7, 0xc8,
		0xc9, 0xca, 0xcb, 0xcc, 0xcd, 0xce, 0xcf, 0xd0}
	callbacks.OnMessage(context.Background(), conn, managedTokenTestMessage(uid, 1))

	done := make(chan int, 1)
	go func() { done <- server.DisconnectTokenConnections(generated.ID) }()
	select {
	case count := <-done:
		if count != 1 {
			t.Fatalf("DisconnectTokenConnections returned %d, want 1", count)
		}
	case <-time.After(time.Second):
		t.Fatal("reentrant OnConnectionClose deadlocked DisconnectTokenConnections")
	}
}

func TestServerStopMakesSessionsInactiveAndClosesAllDespiteErrors(t *testing.T) {
	server, _, generated := newManagedTokenServer(t, nil, Options{})
	firstCallbacks := authenticateManagedToken(t, server, generated.Value)
	secondCallbacks := authenticateManagedToken(t, server, generated.Value)
	first := &recordingConn{disconnectErr: errors.New("first close failed")}
	second := &recordingConn{}
	firstCallbacks.OnConnected(context.Background(), first)
	secondCallbacks.OnConnected(context.Background(), second)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = server.Stop(ctx)
	if first.disconnects() != 1 || second.disconnects() != 1 {
		t.Fatalf("Stop disconnect counts = %d/%d, want 1/1", first.disconnects(), second.disconnects())
	}
	if callbacks := server.authenticateRequest(managedTokenRequest("Bearer " + generated.Value)); !callbacks.Accept {
		t.Fatalf("store-valid post-Stop handshake rejected too early with %d", callbacks.HTTPStatusCode)
	} else {
		late := &recordingConn{}
		callbacks.ConnectionCallbacks.OnConnected(context.Background(), late)
		if late.disconnects() != 1 {
			t.Fatal("stopped connection manager accepted a late tracked session")
		}
	}
}

func TestServerStopWaitsForActiveMessageBeforeDisconnectingAllSessions(t *testing.T) {
	server, _, generated := newManagedTokenServer(t, nil, Options{})
	callbacks := authenticateManagedToken(t, server, generated.Value)
	conn := &recordingConn{sendStarted: make(chan struct{}, 1), sendRelease: make(chan struct{})}
	callbacks.OnConnected(context.Background(), conn)
	uid := []byte{0x71, 0x72, 0x73, 0x74, 0x75, 0x76, 0x77, 0x78,
		0x79, 0x7a, 0x7b, 0x7c, 0x7d, 0x7e, 0x7f, 0x80}
	messageDone := make(chan struct{})
	go func() {
		callbacks.OnMessage(context.Background(), conn, managedTokenTestMessage(uid, 1))
		close(messageDone)
	}()
	select {
	case <-conn.sendStarted:
	case <-time.After(time.Second):
		t.Fatal("message did not reach Connection.Send")
	}

	stopDone := make(chan error, 1)
	go func() { stopDone <- server.Stop(context.Background()) }()
	waitForTokenStopState(t, server.tokens)
	select {
	case <-stopDone:
		close(conn.sendRelease)
		t.Fatal("Server.Stop returned before the active message lease was released")
	case <-time.After(30 * time.Millisecond):
	}
	if conn.disconnects() != 0 {
		close(conn.sendRelease)
		t.Fatal("Server.Stop disconnected the transport before the active message finished")
	}

	close(conn.sendRelease)
	select {
	case <-messageDone:
	case <-time.After(time.Second):
		t.Fatal("active message did not finish after releasing Send")
	}
	select {
	case err := <-stopDone:
		if err != nil {
			t.Fatalf("Server.Stop: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Server.Stop did not finish after the active message")
	}
	if conn.disconnects() != 1 {
		t.Fatalf("Server.Stop disconnect count = %d, want 1", conn.disconnects())
	}
	if got := server.GetConnection(hex.EncodeToString(uid)); got != nil {
		t.Fatal("Server.Stop left the session active")
	}
}

func TestServerStopWaitsForOnConnectionCloseCleanup(t *testing.T) {
	server, store, generated := newManagedTokenServer(t, nil, Options{})
	store.fakeStore.insertEventStarted = make(chan struct{}, 1)
	store.fakeStore.insertEventRelease = make(chan struct{})
	callbacks := authenticateManagedToken(t, server, generated.Value)
	conn := &recordingConn{}
	callbacks.OnConnected(context.Background(), conn)
	uid := []byte{0x61, 0x62, 0x63, 0x64, 0x65, 0x66, 0x67, 0x68,
		0x69, 0x6a, 0x6b, 0x6c, 0x6d, 0x6e, 0x6f, 0x70}
	callbacks.OnMessage(context.Background(), conn, managedTokenTestMessage(uid, 1))

	closeDone := make(chan struct{})
	go func() {
		callbacks.OnConnectionClose(conn)
		close(closeDone)
	}()
	select {
	case <-store.fakeStore.insertEventStarted:
	case <-time.After(time.Second):
		t.Fatal("OnConnectionClose did not reach the disconnected event store call")
	}

	stopDone := make(chan error, 1)
	go func() { stopDone <- server.Stop(context.Background()) }()
	waitForTokenStopState(t, server.tokens)
	select {
	case <-stopDone:
		close(store.fakeStore.insertEventRelease)
		t.Fatal("Server.Stop returned while OnConnectionClose cleanup was blocked")
	case <-time.After(30 * time.Millisecond):
	}
	close(store.fakeStore.insertEventRelease)
	select {
	case <-closeDone:
	case <-time.After(time.Second):
		t.Fatal("OnConnectionClose did not finish after store cleanup was released")
	}
	select {
	case err := <-stopDone:
		if err != nil {
			t.Fatalf("Server.Stop: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Server.Stop did not finish after OnConnectionClose cleanup")
	}
}

func TestDisconnectSessionJoinsConcurrentOnConnectionCloseCleanup(t *testing.T) {
	server, store, generated := newManagedTokenServer(t, nil, Options{})
	store.fakeStore.insertEventStarted = make(chan struct{}, 1)
	store.fakeStore.insertEventRelease = make(chan struct{})
	callbacks := authenticateManagedToken(t, server, generated.Value)
	conn := &recordingConn{}
	callbacks.OnConnected(context.Background(), conn)
	uid := []byte{0x81, 0x82, 0x83, 0x84, 0x85, 0x86, 0x87, 0x88,
		0x89, 0x8a, 0x8b, 0x8c, 0x8d, 0x8e, 0x8f, 0x90}
	callbacks.OnMessage(context.Background(), conn, managedTokenTestMessage(uid, 1))

	server.mu.RLock()
	session := server.conns[hex.EncodeToString(uid)]
	server.mu.RUnlock()
	if session == nil {
		t.Fatal("managed session was not registered")
	}

	closeDone := make(chan struct{})
	go func() {
		callbacks.OnConnectionClose(conn)
		close(closeDone)
	}()
	select {
	case <-store.fakeStore.insertEventStarted:
	case <-time.After(time.Second):
		t.Fatal("OnConnectionClose did not reach the disconnected event store call")
	}

	disconnectDone := make(chan struct{})
	go func() {
		server.disconnectSession(session)
		close(disconnectDone)
	}()
	select {
	case <-disconnectDone:
		close(store.fakeStore.insertEventRelease)
		t.Fatal("disconnectSession returned before concurrent OnConnectionClose cleanup")
	case <-time.After(30 * time.Millisecond):
	}

	close(store.fakeStore.insertEventRelease)
	select {
	case <-closeDone:
	case <-time.After(time.Second):
		t.Fatal("OnConnectionClose did not finish after store cleanup was released")
	}
	select {
	case <-disconnectDone:
	case <-time.After(time.Second):
		t.Fatal("disconnectSession did not join completed OnConnectionClose cleanup")
	}
	if conn.disconnects() != 1 {
		t.Fatalf("disconnect count = %d, want 1", conn.disconnects())
	}
}

func TestDisconnectTokenBeforeOnConnectedRejectsTrackedConnection(t *testing.T) {
	server, store, generated := newManagedTokenServer(t, nil, Options{})
	callbacks := authenticateManagedToken(t, server, generated.Value)
	if got := server.DisconnectTokenConnections(generated.ID); got != 0 {
		t.Fatalf("pre-connect disconnect returned %d, want 0", got)
	}
	conn := &recordingConn{}
	callbacks.OnConnected(context.Background(), conn)
	if conn.disconnects() != 1 {
		t.Fatalf("connection authenticated before tombstone disconnected %d times, want 1", conn.disconnects())
	}
	_, mark := store.counts()
	if mark != 0 {
		t.Fatalf("pre-connect tombstone touched activity %d times", mark)
	}
}

func TestDisconnectTokenWinningBeforeFirstMessagePreventsTouchAndMutation(t *testing.T) {
	server, store, generated := newManagedTokenServer(t, nil, Options{})
	callbacks := authenticateManagedToken(t, server, generated.Value)
	conn := &recordingConn{}
	callbacks.OnConnected(context.Background(), conn)
	server.DisconnectTokenConnections(generated.ID)
	uid := []byte{0x51, 0x52, 0x53, 0x54, 0x55, 0x56, 0x57, 0x58,
		0x59, 0x5a, 0x5b, 0x5c, 0x5d, 0x5e, 0x5f, 0x60}
	callbacks.OnMessage(context.Background(), conn, managedTokenTestMessage(uid, 1))
	_, mark := store.counts()
	store.mu.Lock()
	upserts := len(store.upsertCalls)
	store.mu.Unlock()
	if mark != 0 || upserts != 0 {
		t.Fatalf("revocation winner allowed mark=%d upserts=%d", mark, upserts)
	}
}

func TestDuplicateInstanceUIDKeepsFirstOwnerAndNeverTouchesSecondToken(t *testing.T) {
	server, store, generated := newManagedTokenServer(t, nil, Options{})
	firstCallbacks := authenticateManagedToken(t, server, generated.Value)
	secondCallbacks := authenticateManagedToken(t, server, generated.Value)
	first := &recordingConn{}
	second := &recordingConn{}
	firstCallbacks.OnConnected(context.Background(), first)
	secondCallbacks.OnConnected(context.Background(), second)
	uid := []byte{0x61, 0x62, 0x63, 0x64, 0x65, 0x66, 0x67, 0x68,
		0x69, 0x6a, 0x6b, 0x6c, 0x6d, 0x6e, 0x6f, 0x70}
	uidHex := "6162636465666768696a6b6c6d6e6f70"
	firstCallbacks.OnMessage(context.Background(), first, managedTokenTestMessage(uid, 1))
	secondCallbacks.OnMessage(context.Background(), second, managedTokenTestMessage(uid, 1))

	if got := server.GetConnection(uidHex); got != first {
		t.Fatal("duplicate connection replaced the first UID owner")
	}
	if second.disconnects() != 1 {
		t.Fatalf("duplicate disconnected %d times, want 1", second.disconnects())
	}
	_, mark := store.counts()
	if mark != 1 {
		t.Fatalf("duplicate connection touched activity: total calls = %d, want 1", mark)
	}

	secondCallbacks.OnConnectionClose(second)
	if got := server.GetConnection(uidHex); got != first {
		t.Fatal("rejected duplicate close removed the first UID owner")
	}
}

func TestDuplicateInstanceUIDLateOldCloseDoesNotRemoveNewExactOwner(t *testing.T) {
	server, _, generated := newManagedTokenServer(t, nil, Options{})
	uid := []byte{0x71, 0x72, 0x73, 0x74, 0x75, 0x76, 0x77, 0x78,
		0x79, 0x7a, 0x7b, 0x7c, 0x7d, 0x7e, 0x7f, 0x80}
	uidHex := "7172737475767778797a7b7c7d7e7f80"
	oldCallbacks := authenticateManagedToken(t, server, generated.Value)
	oldConn := &recordingConn{}
	oldCallbacks.OnConnected(context.Background(), oldConn)
	oldCallbacks.OnMessage(context.Background(), oldConn, managedTokenTestMessage(uid, 1))
	oldCallbacks.OnConnectionClose(oldConn)

	newCallbacks := authenticateManagedToken(t, server, generated.Value)
	newConn := &recordingConn{}
	newCallbacks.OnConnected(context.Background(), newConn)
	newCallbacks.OnMessage(context.Background(), newConn, managedTokenTestMessage(uid, 1))
	oldCallbacks.OnConnectionClose(oldConn)

	if got := server.GetConnection(uidHex); got != newConn {
		t.Fatal("late close from old connection removed the new exact owner")
	}
}

func TestDisconnectTokenErrorStillMakesSessionInactiveAndReleasesUID(t *testing.T) {
	server, store, generated := newManagedTokenServer(t, nil, Options{})
	callbacks := authenticateManagedToken(t, server, generated.Value)
	conn := &recordingConn{disconnectErr: errors.New("close failed")}
	callbacks.OnConnected(context.Background(), conn)
	uid := []byte{0x81, 0x82, 0x83, 0x84, 0x85, 0x86, 0x87, 0x88,
		0x89, 0x8a, 0x8b, 0x8c, 0x8d, 0x8e, 0x8f, 0x90}
	uidHex := "8182838485868788898a8b8c8d8e8f90"
	callbacks.OnMessage(context.Background(), conn, managedTokenTestMessage(uid, 1))
	_, markBefore := store.counts()

	if got := server.DisconnectTokenConnections(generated.ID); got != 1 {
		t.Fatalf("DisconnectTokenConnections returned %d, want 1", got)
	}
	if server.GetConnection(uidHex) != nil {
		t.Fatal("Disconnect error left the UID owned")
	}
	callbacks.OnMessage(context.Background(), conn, managedTokenTestMessage(uid, 2))
	_, markAfter := store.counts()
	if markAfter != markBefore {
		t.Fatalf("inactive session touched activity after Disconnect error: %d -> %d", markBefore, markAfter)
	}
	if err := server.sendToInstance(context.Background(), uidHex, &protobufs.ServerToAgent{InstanceUid: uid}); err == nil {
		t.Fatal("inactive session accepted a push after Disconnect error")
	}
}

func TestOpAMPAuthMarkFailureRemovesReservationBeforeBusinessMutation(t *testing.T) {
	server, store, generated := newManagedTokenServer(t, nil, Options{})
	store.markErr = errors.New("database unavailable")
	callbacks := authenticateManagedToken(t, server, generated.Value)
	conn := &recordingConn{}
	callbacks.OnConnected(context.Background(), conn)
	uid := []byte{0x91, 0x92, 0x93, 0x94, 0x95, 0x96, 0x97, 0x98,
		0x99, 0x9a, 0x9b, 0x9c, 0x9d, 0x9e, 0x9f, 0xa0}
	uidHex := "9192939495969798999a9b9c9d9e9fa0"
	callbacks.OnMessage(context.Background(), conn, managedTokenTestMessage(uid, 1))

	if server.GetConnection(uidHex) != nil {
		t.Fatal("failed activity touch left UID reservation behind")
	}
	store.mu.Lock()
	upserts := len(store.upsertCalls)
	store.mu.Unlock()
	if upserts != 0 {
		t.Fatalf("failed activity touch caused %d workload upserts", upserts)
	}
	if conn.disconnects() != 1 {
		t.Fatalf("failed activity touch disconnected %d times, want 1", conn.disconnects())
	}
}

// TestOnMessage_UnknownInstance_RequestsFullState guards the resync path:
// when an agent sends a heartbeat for a UID we have no record of (typical
// after a server restart with ephemeral DB), we must set ReportFullState so
// the agent re-sends its AgentDescription and the workload can be bootstrapped.
func TestOnMessage_UnknownInstance_RequestsFullState(t *testing.T) {
	srv := New(nil, nil, Options{})
	uid := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}

	reply := srv.handleAcceptedMessage(&protobufs.AgentToServer{
		InstanceUid: uid,
		SequenceNum: 5,
	})

	if reply == nil {
		t.Fatal("onMessage returned nil reply")
	}
	wantFlag := uint64(protobufs.ServerToAgentFlags_ServerToAgentFlags_ReportFullState)
	if reply.Flags&wantFlag == 0 {
		t.Errorf("expected ReportFullState flag set, got Flags=0x%x", reply.Flags)
	}
}

// TestOnMessage_KnownInstance_DoesNotRequestFullState is the regression guard:
// once the registry knows the instance, subsequent heartbeats must not carry
// the ReportFullState flag (we already have the state we need).
func TestOnMessage_KnownInstance_DoesNotRequestFullState(t *testing.T) {
	srv := New(nil, nil, Options{})
	uid := []byte{0x10, 0x20, 0x30, 0x40, 0x50, 0x60, 0x70, 0x80,
		0x90, 0xa0, 0xb0, 0xc0, 0xd0, 0xe0, 0xf0, 0x11}

	// Seed the registry with an AgentDescription-bearing message.
	_ = srv.handleAcceptedMessage(&protobufs.AgentToServer{
		InstanceUid: uid,
		SequenceNum: 1,
		AgentDescription: &protobufs.AgentDescription{
			IdentifyingAttributes: []*protobufs.KeyValue{
				{
					Key: "service.name",
					Value: &protobufs.AnyValue{
						Value: &protobufs.AnyValue_StringValue{StringValue: "otelcol"},
					},
				},
			},
		},
	})

	reply := srv.handleAcceptedMessage(&protobufs.AgentToServer{
		InstanceUid: uid,
		SequenceNum: 2,
	})

	if reply == nil {
		t.Fatal("onMessage returned nil reply")
	}
	flag := uint64(protobufs.ServerToAgentFlags_ServerToAgentFlags_ReportFullState)
	if reply.Flags&flag != 0 {
		t.Errorf("known-instance heartbeat must not request full state, got Flags=0x%x", reply.Flags)
	}
}
