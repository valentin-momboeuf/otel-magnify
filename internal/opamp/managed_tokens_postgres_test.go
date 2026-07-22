package opamp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	opampClient "github.com/open-telemetry/opamp-go/client"
	clientTypes "github.com/open-telemetry/opamp-go/client/types"
	"github.com/open-telemetry/opamp-go/protobufs"

	"github.com/magnify-labs/otel-magnify/internal/opampauth"
	"github.com/magnify-labs/otel-magnify/internal/store"
	"github.com/magnify-labs/otel-magnify/internal/testdb"
	"github.com/magnify-labs/otel-magnify/pkg/ext"
	"github.com/magnify-labs/otel-magnify/pkg/models"
)

type managedTokenPostgresServer struct {
	opamp *Server
	http  *httptest.Server
	url   string
}

func startManagedTokenPostgresServer(t *testing.T, db *store.DB) *managedTokenPostgresServer {
	t.Helper()
	opampServer := New(db, nil, Options{DisconnectGrace: 20 * time.Millisecond})
	handler, connContext, err := opampServer.Attach()
	if err != nil {
		t.Fatalf("attach OpAMP server: %v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/opamp", handler)
	httpServer := httptest.NewUnstartedServer(mux)
	httpServer.Config.ConnContext = connContext
	httpServer.Start()
	return &managedTokenPostgresServer{
		opamp: opampServer,
		http:  httpServer,
		url:   "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/v1/opamp",
	}
}

func (s *managedTokenPostgresServer) Stop(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := s.opamp.Stop(ctx); err != nil {
		t.Errorf("stop OpAMP server: %v", err)
	}
	s.http.Close()
}

type managedTokenPostgresClient struct {
	client    opampClient.OpAMPClient
	connected chan struct{}
}

func connectManagedTokenPostgresClient(
	t *testing.T,
	serverURL string,
	value string,
	uid clientTypes.InstanceUid,
) *managedTokenPostgresClient {
	t.Helper()
	client := opampClient.NewWebSocket(nil)
	if err := client.SetAgentDescription(&protobufs.AgentDescription{
		IdentifyingAttributes: []*protobufs.KeyValue{{
			Key: "service.name",
			Value: &protobufs.AnyValue{
				Value: &protobufs.AnyValue_StringValue{StringValue: "otelcol-postgres-contract"},
			},
		}},
	}); err != nil {
		t.Fatalf("set agent description: %v", err)
	}
	connected := make(chan struct{}, 1)
	heartbeat := time.Duration(0)
	if err := client.Start(context.Background(), clientTypes.StartSettings{
		OpAMPServerURL:    serverURL,
		Header:            http.Header{"Authorization": []string{"Bearer " + value}},
		InstanceUid:       uid,
		HeartbeatInterval: &heartbeat,
		Callbacks: clientTypes.Callbacks{
			OnConnect: func(context.Context) {
				select {
				case connected <- struct{}{}:
				default:
				}
			},
		},
	}); err != nil {
		t.Fatalf("start OpAMP WebSocket client: %v", err)
	}
	return &managedTokenPostgresClient{client: client, connected: connected}
}

func (c *managedTokenPostgresClient) WaitConnected(t *testing.T) {
	t.Helper()
	select {
	case <-c.connected:
	case <-time.After(5 * time.Second):
		t.Fatal("OpAMP WebSocket client did not connect")
	}
}

func (c *managedTokenPostgresClient) Stop(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := c.client.Stop(ctx); err != nil {
		t.Errorf("stop OpAMP WebSocket client: %v", err)
	}
}

func createManagedTokenPostgresCredential(
	t *testing.T,
	db *store.DB,
	name string,
	createdAt time.Time,
	expiresAt *time.Time,
) opampauth.GeneratedToken {
	t.Helper()
	generated, err := opampauth.Generate()
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	credential := models.OpAMPTokenCredential{
		Token: models.OpAMPToken{
			ID:        generated.ID,
			Name:      name,
			CreatedAt: createdAt,
			CreatedBy: "postgres-contract",
			ExpiresAt: expiresAt,
			Status:    models.OpAMPTokenActive,
		},
		SecretHash: generated.SecretHash,
	}
	event := ext.AuditEvent{
		EventID:    uuid.NewString(),
		OccurredAt: createdAt,
		Action:     "opamp.token.create",
		UserID:     credential.Token.CreatedBy,
		Resource:   "opamp_token",
		ResourceID: generated.ID,
	}
	if err := db.CreateOpAMPToken(context.Background(), credential, event); err != nil {
		t.Fatalf("create managed token %s: %v", name, err)
	}
	return generated
}

func revokeManagedTokenPostgresCredential(t *testing.T, db *store.DB, id string, now time.Time) {
	t.Helper()
	event := ext.AuditEvent{
		EventID:    uuid.NewString(),
		OccurredAt: now,
		Action:     "opamp.token.revoke",
		UserID:     "postgres-contract",
		Resource:   "opamp_token",
		ResourceID: id,
	}
	if _, changed, err := db.RevokeOpAMPToken(context.Background(), id, event.UserID, now, event); err != nil {
		t.Fatalf("revoke managed token: %v", err)
	} else if !changed {
		t.Fatal("active managed token was not transitioned to revoked")
	}
}

func waitForManagedTokenCondition(t *testing.T, timeout time.Duration, description string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		select {
		case <-time.After(10 * time.Millisecond):
		case <-t.Context().Done():
			t.Fatalf("waiting for %s: %v", description, t.Context().Err())
		}
	}
	t.Fatalf("condition not met before timeout: %s", description)
}

func managedTokenMetadataByID(t *testing.T, db *store.DB, id string) models.OpAMPToken {
	t.Helper()
	tokens, err := db.ListOpAMPTokens(context.Background(), time.Now().UTC())
	if err != nil {
		t.Fatalf("list managed tokens: %v", err)
	}
	for _, token := range tokens {
		if token.ID == id {
			return token
		}
	}
	t.Fatalf("managed token metadata missing for ID %s", id)
	return models.OpAMPToken{}
}

func assertManagedTokenWebSocketRejected(t *testing.T, serverURL string, header http.Header) {
	t.Helper()
	conn, response, err := websocket.DefaultDialer.Dial(serverURL, header)
	if conn != nil {
		_ = conn.Close()
		t.Fatal("invalid managed token WebSocket handshake succeeded")
	}
	if err == nil || response == nil {
		t.Fatalf("invalid managed token handshake did not return an HTTP response: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("invalid managed token HTTP status = %d, want 401", response.StatusCode)
	}
	if got := response.Header.Get("WWW-Authenticate"); got != `Bearer realm="opamp"` {
		t.Fatalf("invalid managed token challenge = %q", got)
	}
}

func TestManagedTokensPostgresPersistAcrossApplicationPoolRestart(t *testing.T) {
	database := testdb.New(t)
	pool := store.PoolConfig{MaxOpenConns: 4, MaxIdleConns: 2, ConnMaxLifetime: time.Minute}
	db, err := store.Open(database.DSN, pool)
	if err != nil {
		t.Fatalf("open first PostgreSQL pool: %v", err)
	}
	if err := db.Migrate(); err != nil {
		_ = db.Close()
		t.Fatalf("migrate PostgreSQL schema: %v", err)
	}

	createdAt := time.Now().UTC()
	expiresAt := createdAt.Add(3 * time.Second)
	active := createManagedTokenPostgresCredential(t, db, "active", createdAt, nil)
	expiring := createManagedTokenPostgresCredential(t, db, "expiring", createdAt, &expiresAt)
	revoking := createManagedTokenPostgresCredential(t, db, "revoking", createdAt, nil)
	firstServer := startManagedTokenPostgresServer(t, db)

	clients := []*managedTokenPostgresClient{
		connectManagedTokenPostgresClient(t, firstServer.url, active.Value, clientTypes.InstanceUid{0x01}),
		connectManagedTokenPostgresClient(t, firstServer.url, expiring.Value, clientTypes.InstanceUid{0x02}),
		connectManagedTokenPostgresClient(t, firstServer.url, revoking.Value, clientTypes.InstanceUid{0x03}),
	}
	for _, client := range clients {
		client.WaitConnected(t)
	}
	waitForManagedTokenCondition(t, 5*time.Second, "three accepted first messages", func() bool {
		return firstServer.opamp.ConnectedInstanceCount() == 3 &&
			managedTokenMetadataByID(t, db, active.ID).LastUsedAt != nil &&
			managedTokenMetadataByID(t, db, expiring.ID).LastUsedAt != nil &&
			managedTokenMetadataByID(t, db, revoking.ID).LastUsedAt != nil
	})
	activeLastUsed := *managedTokenMetadataByID(t, db, active.ID).LastUsedAt

	revokedAt := time.Now().UTC()
	revokeManagedTokenPostgresCredential(t, db, revoking.ID, revokedAt)
	if disconnected := firstServer.opamp.DisconnectTokenConnections(revoking.ID); disconnected != 1 {
		t.Fatalf("revocation disconnected %d live sessions, want 1", disconnected)
	}
	waitForManagedTokenCondition(t, 2*time.Second, "revoked connection removal", func() bool {
		return firstServer.opamp.ConnectedInstanceCount() == 2
	})
	waitForManagedTokenCondition(t, 5*time.Second, "hot expiry connection removal", func() bool {
		return firstServer.opamp.ConnectedInstanceCount() == 1
	})

	for _, client := range clients {
		client.Stop(t)
	}
	firstServer.Stop(t)
	if err := db.Close(); err != nil {
		t.Fatalf("close first PostgreSQL pool: %v", err)
	}

	db, err = store.Open(database.DSN, pool)
	if err != nil {
		t.Fatalf("reopen PostgreSQL pool: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate reopened PostgreSQL schema: %v", err)
	}
	secondServer := startManagedTokenPostgresServer(t, db)
	t.Cleanup(func() { secondServer.Stop(t) })

	assertManagedTokenWebSocketRejected(t, secondServer.url, nil)
	assertManagedTokenWebSocketRejected(t, secondServer.url, http.Header{"Authorization": []string{"Bearer invalid"}})
	assertManagedTokenWebSocketRejected(t, secondServer.url, http.Header{"Authorization": []string{"Bearer " + revoking.Value}})
	assertManagedTokenWebSocketRejected(t, secondServer.url, http.Header{"Authorization": []string{"Bearer " + expiring.Value}})

	activeAfterRestart := managedTokenMetadataByID(t, db, active.ID)
	if activeAfterRestart.LastUsedAt == nil || !activeAfterRestart.LastUsedAt.Equal(activeLastUsed) {
		t.Fatalf("persisted active last_used_at changed across pool restart: got %v, want %v", activeAfterRestart.LastUsedAt, activeLastUsed)
	}
	if got := managedTokenMetadataByID(t, db, revoking.ID).Status; got != models.OpAMPTokenRevoked {
		t.Fatalf("revoked status after pool restart = %q", got)
	}
	if got := managedTokenMetadataByID(t, db, expiring.ID).Status; got != models.OpAMPTokenExpired {
		t.Fatalf("expired status after pool restart = %q", got)
	}

	reconnected := connectManagedTokenPostgresClient(t, secondServer.url, active.Value, clientTypes.InstanceUid{0x04})
	reconnected.WaitConnected(t)
	waitForManagedTokenCondition(t, 5*time.Second, "active token reconnect after pool restart", func() bool {
		return secondServer.opamp.ConnectedInstanceCount() == 1
	})
	reconnected.Stop(t)
}
