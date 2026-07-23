package api

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/magnify-labs/otel-magnify/internal/auth"
	"github.com/magnify-labs/otel-magnify/internal/opamp"
	"github.com/magnify-labs/otel-magnify/internal/opampauth"
	"github.com/magnify-labs/otel-magnify/internal/store"
	"github.com/magnify-labs/otel-magnify/internal/testdb"
	"github.com/magnify-labs/otel-magnify/pkg/ext"
	"github.com/magnify-labs/otel-magnify/pkg/models"
)

type opAMPTokenStoreDouble struct {
	ext.Store
	listFn   func(context.Context, time.Time) ([]models.OpAMPToken, error)
	createFn func(context.Context, models.OpAMPTokenCredential, ext.AuditEvent) error
	revokeFn func(context.Context, string, string, time.Time, ext.AuditEvent) (models.OpAMPToken, bool, error)
}

func (s opAMPTokenStoreDouble) ListOpAMPTokens(ctx context.Context, now time.Time) ([]models.OpAMPToken, error) {
	if s.listFn != nil {
		return s.listFn(ctx, now)
	}
	return s.Store.ListOpAMPTokens(ctx, now)
}

func (s opAMPTokenStoreDouble) CreateOpAMPToken(ctx context.Context, credential models.OpAMPTokenCredential, event ext.AuditEvent) error {
	if s.createFn != nil {
		return s.createFn(ctx, credential, event)
	}
	return s.Store.CreateOpAMPToken(ctx, credential, event)
}

func (s opAMPTokenStoreDouble) RevokeOpAMPToken(ctx context.Context, id, actor string, now time.Time, event ext.AuditEvent) (models.OpAMPToken, bool, error) {
	if s.revokeFn != nil {
		return s.revokeFn(ctx, id, actor, now, event)
	}
	return s.Store.RevokeOpAMPToken(ctx, id, actor, now, event)
}

func TestOpAMPTokenRoutesRequireAdministrator(t *testing.T) {
	_, router, _ := newOpAMPTokenTestAPI(t, nil, nil)
	future := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	createBody := fmt.Sprintf(`{"name":"admin","expires_at":%q}`, future)
	routes := []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodPost, path: "/api/v1/opamp/tokens", body: createBody},
		{method: http.MethodGet, path: "/api/v1/opamp/tokens"},
		{method: http.MethodPost, path: "/api/v1/opamp/tokens/00000000-0000-4000-8000-000000000001/revoke"},
	}

	for _, route := range routes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			req := httptest.NewRequest(route.method, route.path, strings.NewReader(route.body))
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("anonymous status = %d, body = %s; want 401", rec.Code, rec.Body.String())
			}

			for _, group := range []string{"viewer", "editor"} {
				req = authedRequestForGroups(t, route.method, route.path, route.body, []string{group})
				rec = httptest.NewRecorder()
				router.ServeHTTP(rec, req)
				if rec.Code != http.StatusForbidden {
					t.Fatalf("%s status = %d, body = %s; want 403", group, rec.Code, rec.Body.String())
				}
			}

			req = authedRequestForGroups(t, route.method, route.path, route.body, []string{"administrator"})
			rec = httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden {
				t.Fatalf("administrator status = %d, body = %s; want route authorization", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestOpAMPTokenCreateListAndIdempotentRevokeLifecycle(t *testing.T) {
	db, router, pusher := newOpAMPTokenTestAPI(t, nil, func(logger *recordingAuditLogger) {
		logger.failWith(errors.New("enterprise sink unavailable"))
	})

	emptyRec := serveOpAMPTokenRequest(t, router, http.MethodGet, "/api/v1/opamp/tokens", "", []string{"administrator"})
	if emptyRec.Code != http.StatusOK || strings.TrimSpace(emptyRec.Body.String()) != `{"tokens":[]}` {
		t.Fatalf("empty list = (%d, %s), want 200 with non-nil []", emptyRec.Code, emptyRec.Body.String())
	}

	expiry := time.Now().UTC().Add(3 * time.Hour).Truncate(time.Second).In(time.FixedZone("request-offset", 2*60*60))
	body := fmt.Sprintf(
		`{"name":"  production-eu  ","description":"Collectors operated by the EU platform team","team":"platform","environment":"production","expires_at":%q}`,
		expiry.Format(time.RFC3339),
	)
	createRec := serveOpAMPTokenRequest(t, router, http.MethodPost, "/api/v1/opamp/tokens", body, []string{"administrator"})
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s; want 201", createRec.Code, createRec.Body.String())
	}
	if got := createRec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	if got := createRec.Header().Get("Pragma"); got != "no-cache" {
		t.Fatalf("Pragma = %q, want no-cache", got)
	}
	var created struct {
		Token models.OpAMPToken `json:"token"`
		Value string            `json:"value"`
	}
	decodeOpAMPTokenResponse(t, createRec, &created)
	if created.Token.Name != "production-eu" || created.Token.CreatedBy != "user-001" || created.Token.Status != models.OpAMPTokenActive {
		t.Fatalf("created token metadata = %+v", created.Token)
	}
	if created.Token.ExpiresAt == nil || created.Token.ExpiresAt.Location() != time.UTC || !created.Token.ExpiresAt.Equal(expiry) {
		t.Fatalf("expires_at = %v, want UTC %v", created.Token.ExpiresAt, expiry.UTC())
	}
	if !strings.HasPrefix(created.Value, "ompt_"+created.Token.ID+".") {
		t.Fatalf("raw value does not carry public ID: %q", created.Value)
	}
	if strings.Contains(createRec.Body.String(), "secret_hash") {
		t.Fatalf("create response leaked secret hash: %s", createRec.Body.String())
	}

	event := readOpAMPTokenAuditEvent(t, db, created.Token.ID, "opamp.token.create")
	assertExactOpAMPTokenAuditEvent(t, event, created.Token, "opamp.token.create", "user-001", "admin@test.com", created.Token.CreatedAt)
	if event.EventID == created.Token.ID {
		t.Fatal("audit event ID must be distinct from token ID")
	}
	if event.Detail != "" || strings.Contains(fmt.Sprintf("%+v", event), created.Value) {
		t.Fatalf("audit event leaked token material: %+v", event)
	}

	listRec := serveOpAMPTokenRequest(t, router, http.MethodGet, "/api/v1/opamp/tokens", "", []string{"administrator"})
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", listRec.Code, listRec.Body.String())
	}
	if strings.Contains(listRec.Body.String(), created.Value) || strings.Contains(listRec.Body.String(), `"value"`) || strings.Contains(listRec.Body.String(), "secret_hash") {
		t.Fatalf("list response leaked credentials: %s", listRec.Body.String())
	}
	var listed struct {
		Tokens []models.OpAMPToken `json:"tokens"`
	}
	decodeOpAMPTokenResponse(t, listRec, &listed)
	if len(listed.Tokens) != 1 || listed.Tokens[0].ID != created.Token.ID {
		t.Fatalf("listed tokens = %+v", listed.Tokens)
	}

	pusher.disconnectMu.Lock()
	pusher.tokenConnections = map[string]int{created.Token.ID: 2}
	pusher.disconnectMu.Unlock()
	revokePath := "/api/v1/opamp/tokens/" + created.Token.ID + "/revoke"
	revokeRec := serveOpAMPTokenRequest(t, router, http.MethodPost, revokePath, "", []string{"administrator"})
	if revokeRec.Code != http.StatusOK {
		t.Fatalf("revoke status = %d, body = %s", revokeRec.Code, revokeRec.Body.String())
	}
	var revoked struct {
		Token                   models.OpAMPToken `json:"token"`
		DisconnectedConnections int               `json:"disconnected_connections"`
	}
	decodeOpAMPTokenResponse(t, revokeRec, &revoked)
	if revoked.DisconnectedConnections != 2 || revoked.Token.RevokedAt == nil || revoked.Token.RevokedBy != "user-001" || revoked.Token.Status != models.OpAMPTokenRevoked {
		t.Fatalf("revoke response = %+v", revoked)
	}
	revokeEvent := readOpAMPTokenAuditEvent(t, db, created.Token.ID, "opamp.token.revoke")
	assertExactOpAMPTokenAuditEvent(t, revokeEvent, revoked.Token, "opamp.token.revoke", "user-001", "admin@test.com", *revoked.Token.RevokedAt)

	repeatRec := serveOpAMPTokenRequest(t, router, http.MethodPost, revokePath, "", []string{"administrator"})
	if repeatRec.Code != http.StatusOK {
		t.Fatalf("repeat revoke status = %d, body = %s", repeatRec.Code, repeatRec.Body.String())
	}
	var repeated struct {
		Token                   models.OpAMPToken `json:"token"`
		DisconnectedConnections int               `json:"disconnected_connections"`
	}
	decodeOpAMPTokenResponse(t, repeatRec, &repeated)
	if repeated.DisconnectedConnections != 0 || repeated.Token.RevokedAt == nil ||
		!repeated.Token.RevokedAt.Equal(revoked.Token.RevokedAt.Truncate(time.Microsecond)) {
		t.Fatalf("repeat revoke response = %+v", repeated)
	}
	if got := countOpAMPTokenAuditEvents(t, db, created.Token.ID, "opamp.token.revoke"); got != 1 {
		t.Fatalf("revoke audit events = %d, want 1", got)
	}
	pusher.disconnectMu.Lock()
	disconnected := pusher.disconnected
	pusher.disconnectMu.Unlock()
	if disconnected != 2 {
		t.Fatalf("actual disconnected connections = %d, want 2", disconnected)
	}
}

func TestOpAMPTokenCreateRejectsInvalidOrOversizedBodies(t *testing.T) {
	future := time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339)
	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{name: "unknown_field", body: fmt.Sprintf(`{"name":"ok","expires_at":%q,"extra":true}`, future), wantStatus: http.StatusBadRequest},
		{name: "trailing_value", body: fmt.Sprintf(`{"name":"ok","expires_at":%q} {}`, future), wantStatus: http.StatusBadRequest},
		{name: "over_8_kib", body: `{"name":"ok","padding":"` + strings.Repeat("x", 8192) + `"}`, wantStatus: http.StatusRequestEntityTooLarge},
		{name: "blank_name", body: `{"name":"   "}`, wantStatus: http.StatusBadRequest},
		{name: "name_129_runes", body: fmt.Sprintf(`{"name":%q}`, strings.Repeat("é", 129)), wantStatus: http.StatusBadRequest},
		{name: "description_513_runes", body: fmt.Sprintf(`{"name":"ok","description":%q}`, strings.Repeat("é", 513)), wantStatus: http.StatusBadRequest},
		{name: "team_129_runes", body: fmt.Sprintf(`{"name":"ok","team":%q}`, strings.Repeat("é", 129)), wantStatus: http.StatusBadRequest},
		{name: "environment_129_runes", body: fmt.Sprintf(`{"name":"ok","environment":%q}`, strings.Repeat("é", 129)), wantStatus: http.StatusBadRequest},
		{name: "expiration_not_future", body: fmt.Sprintf(`{"name":"ok","expires_at":%q}`, time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)), wantStatus: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, router, _ := newOpAMPTokenTestAPI(t, nil, nil)
			rec := serveOpAMPTokenRequest(t, router, http.MethodPost, "/api/v1/opamp/tokens", tt.body, []string{"administrator"})
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, body = %s; want %d", rec.Code, rec.Body.String(), tt.wantStatus)
			}
		})
	}
}

func TestOpAMPTokenStoreFailuresAreGenericAndMissingIDIs404(t *testing.T) {
	db, _, _ := newOpAMPTokenTestAPI(t, nil, nil)
	storeDouble := opAMPTokenStoreDouble{
		Store: db,
		listFn: func(context.Context, time.Time) ([]models.OpAMPToken, error) {
			return nil, errors.New("postgres secret DSN")
		},
	}
	_, router, _ := newOpAMPTokenTestAPI(t, storeDouble, nil)
	listRec := serveOpAMPTokenRequest(t, router, http.MethodGet, "/api/v1/opamp/tokens", "", []string{"administrator"})
	if listRec.Code != http.StatusServiceUnavailable || strings.Contains(listRec.Body.String(), "postgres secret DSN") {
		t.Fatalf("store failure response = (%d, %s)", listRec.Code, listRec.Body.String())
	}

	missingRec := serveOpAMPTokenRequest(t, router, http.MethodPost, "/api/v1/opamp/tokens/not-a-uuid/revoke", "", []string{"administrator"})
	if missingRec.Code != http.StatusNotFound {
		t.Fatalf("missing ID status = %d, body = %s; want 404", missingRec.Code, missingRec.Body.String())
	}
}

func TestOpAMPTokenAtomicOutboxFailureRollsBackMutation(t *testing.T) {
	t.Run("create", func(t *testing.T) {
		db, router, _ := newOpAMPTokenTestAPI(t, nil, nil)
		installRejectOpAMPTokenAuditTrigger(t, db)
		rec := serveOpAMPTokenRequest(t, router, http.MethodPost, "/api/v1/opamp/tokens", `{"name":"rollback-create"}`, []string{"administrator"})
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("create status = %d, body = %s; want 503", rec.Code, rec.Body.String())
		}
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM opamp_tokens`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("token count = %d, want 0 after outbox rollback", count)
		}
	})

	t.Run("revoke", func(t *testing.T) {
		db, router, pusher := newOpAMPTokenTestAPI(t, nil, nil)
		createRec := serveOpAMPTokenRequest(t, router, http.MethodPost, "/api/v1/opamp/tokens", `{"name":"rollback-revoke"}`, []string{"administrator"})
		var created struct {
			Token models.OpAMPToken `json:"token"`
		}
		decodeOpAMPTokenResponse(t, createRec, &created)
		installRejectOpAMPTokenAuditTrigger(t, db)
		pusher.disconnectMu.Lock()
		pusher.tokenConnections = map[string]int{created.Token.ID: 1}
		pusher.disconnectMu.Unlock()

		rec := serveOpAMPTokenRequest(t, router, http.MethodPost, "/api/v1/opamp/tokens/"+created.Token.ID+"/revoke", "", []string{"administrator"})
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("revoke status = %d, body = %s; want 503", rec.Code, rec.Body.String())
		}
		var revokedAt *time.Time
		if err := db.QueryRow(`SELECT revoked_at FROM opamp_tokens WHERE id = ?`, created.Token.ID).Scan(&revokedAt); err != nil {
			t.Fatal(err)
		}
		if revokedAt != nil {
			t.Fatalf("revoked_at = %v, want NULL after outbox rollback", revokedAt)
		}
		pusher.disconnectMu.Lock()
		disconnected := pusher.disconnected
		pusher.disconnectMu.Unlock()
		if disconnected != 0 {
			t.Fatalf("known failed revoke disconnected %d connections, want 0", disconnected)
		}
	})
}

func TestOpAMPTokenUnknownCreateReportsOnlyPublicReconciliationData(t *testing.T) {
	db, _, _ := newOpAMPTokenTestAPI(t, nil, nil)
	var captured models.OpAMPTokenCredential
	storeDouble := opAMPTokenStoreDouble{
		Store: db,
		createFn: func(_ context.Context, credential models.OpAMPTokenCredential, _ ext.AuditEvent) error {
			captured = credential
			return fmt.Errorf("commit transport: %w", ext.ErrCommitOutcomeUnknown)
		},
	}
	_, router, _ := newOpAMPTokenTestAPI(t, storeDouble, nil)
	rec := serveOpAMPTokenRequest(t, router, http.MethodPost, "/api/v1/opamp/tokens", `{"name":"unknown-create"}`, []string{"administrator"})
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s; want 503", rec.Code, rec.Body.String())
	}
	var body map[string]string
	decodeOpAMPTokenResponse(t, rec, &body)
	if len(body) != 3 || body["error"] != "operation outcome unknown" || body["side_effect_status"] != "unknown" || body["token_id"] != captured.Token.ID {
		t.Fatalf("unknown create body = %v", body)
	}
	if strings.Contains(rec.Body.String(), "ompt_") || strings.Contains(rec.Body.String(), fmt.Sprintf("%x", captured.SecretHash)) {
		t.Fatalf("unknown create leaked token material: %s", rec.Body.String())
	}
}

func TestOpAMPTokenUnknownRevokeFailsClosedBeforeResponse(t *testing.T) {
	db, router, _ := newOpAMPTokenTestAPI(t, nil, nil)
	createRec := serveOpAMPTokenRequest(t, router, http.MethodPost, "/api/v1/opamp/tokens", `{"name":"unknown-revoke"}`, []string{"administrator"})
	var created struct {
		Token models.OpAMPToken `json:"token"`
	}
	decodeOpAMPTokenResponse(t, createRec, &created)

	storeDouble := opAMPTokenStoreDouble{
		Store: db,
		revokeFn: func(context.Context, string, string, time.Time, ext.AuditEvent) (models.OpAMPToken, bool, error) {
			return models.OpAMPToken{}, false, fmt.Errorf("wrapped: %w", ext.ErrCommitOutcomeUnknown)
		},
	}
	pusher := &fakeOpAMPPusher{
		instances:        make(map[string][]opamp.Instance),
		tokenConnections: map[string]int{created.Token.ID: 3},
	}
	_, unknownRouter, _ := newOpAMPTokenTestAPIWithPusher(t, storeDouble, pusher, nil)
	rec := serveOpAMPTokenRequest(t, unknownRouter, http.MethodPost, "/api/v1/opamp/tokens/"+created.Token.ID+"/revoke", "", []string{"administrator"})
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s; want 503", rec.Code, rec.Body.String())
	}
	var body map[string]string
	decodeOpAMPTokenResponse(t, rec, &body)
	if len(body) != 3 || body["error"] != "operation outcome unknown" || body["side_effect_status"] != "unknown" || body["token_id"] != created.Token.ID {
		t.Fatalf("unknown revoke body = %v", body)
	}
	pusher.disconnectMu.Lock()
	disconnected := pusher.disconnected
	pusher.disconnectMu.Unlock()
	if disconnected != 3 {
		t.Fatalf("unknown revoke disconnected %d connections, want pessimistic 3", disconnected)
	}
}

func TestOpAMPTokenConcurrentRevokeLoserJoinsDisableBeforeResponse(t *testing.T) {
	db, router, pusher := newOpAMPTokenTestAPI(t, nil, nil)
	createRec := serveOpAMPTokenRequest(t, router, http.MethodPost, "/api/v1/opamp/tokens", `{"name":"concurrent-revoke"}`, []string{"administrator"})
	var created struct {
		Token models.OpAMPToken `json:"token"`
	}
	decodeOpAMPTokenResponse(t, createRec, &created)

	started := make(chan string, 1)
	release := make(chan struct{})
	pusher.disconnectMu.Lock()
	pusher.tokenConnections = map[string]int{created.Token.ID: 4}
	pusher.disconnectStarted = started
	pusher.disconnectRelease = release
	pusher.disconnectMu.Unlock()

	path := "/api/v1/opamp/tokens/" + created.Token.ID + "/revoke"
	results := make(chan *httptest.ResponseRecorder, 2)
	var workers sync.WaitGroup
	workers.Add(2)
	for range 2 {
		go func() {
			defer workers.Done()
			results <- serveOpAMPTokenRequest(t, router, http.MethodPost, path, "", []string{"administrator"})
		}()
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("winning revoke did not begin in-memory disable")
	}
	select {
	case rec := <-results:
		t.Fatalf("revoke responded before disable completed: %d %s", rec.Code, rec.Body.String())
	case <-time.After(150 * time.Millisecond):
	}
	close(release)
	workers.Wait()
	close(results)

	totalDisconnected := 0
	for rec := range results {
		if rec.Code != http.StatusOK {
			t.Fatalf("concurrent revoke status = %d, body = %s", rec.Code, rec.Body.String())
		}
		var response struct {
			DisconnectedConnections int `json:"disconnected_connections"`
		}
		decodeOpAMPTokenResponse(t, rec, &response)
		totalDisconnected += response.DisconnectedConnections
	}
	if totalDisconnected != 4 {
		t.Fatalf("reported disconnected connections = %d, want 4 total", totalDisconnected)
	}
	if got := countOpAMPTokenAuditEvents(t, db, created.Token.ID, "opamp.token.revoke"); got != 1 {
		t.Fatalf("concurrent revoke events = %d, want 1", got)
	}
	pusher.disconnectMu.Lock()
	disconnected := pusher.disconnected
	pusher.disconnectMu.Unlock()
	if disconnected != 4 {
		t.Fatalf("actual disconnected connections = %d, want 4", disconnected)
	}
}

func newOpAMPTokenTestAPI(t *testing.T, database ext.Store, configureAudit func(*recordingAuditLogger)) (*store.DB, http.Handler, *fakeOpAMPPusher) {
	t.Helper()
	return newOpAMPTokenTestAPIWithPusher(t, database, &fakeOpAMPPusher{instances: make(map[string][]opamp.Instance)}, configureAudit)
}

func newOpAMPTokenTestAPIWithPusher(t *testing.T, database ext.Store, pusher *fakeOpAMPPusher, configureAudit func(*recordingAuditLogger)) (*store.DB, http.Handler, *fakeOpAMPPusher) {
	t.Helper()
	postgres, err := store.Open(testdb.New(t).DSN, testPoolConfig())
	if err != nil {
		t.Fatal(err)
	}
	if err := postgres.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = postgres.Close() })
	if database == nil {
		database = postgres
	}
	logger := &recordingAuditLogger{}
	if configureAudit != nil {
		configureAudit(logger)
	}
	router := NewRouter(
		database,
		auth.New("test-secret-key-at-least-32-bytes!"),
		nil,
		pusher,
		logger,
		"",
		nil,
		nil,
		30*24*time.Hour,
		testEnabledFeatures(),
		nil,
		nil,
	)
	return postgres, router, pusher
}

func serveOpAMPTokenRequest(t *testing.T, router http.Handler, method, path, body string, groups []string) *httptest.ResponseRecorder {
	t.Helper()
	req := authedRequestForGroups(t, method, path, body, groups)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func decodeOpAMPTokenResponse(t *testing.T, rec *httptest.ResponseRecorder, dst any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), dst); err != nil {
		t.Fatalf("decode response %q: %v", rec.Body.String(), err)
	}
}

func readOpAMPTokenAuditEvent(t *testing.T, db *store.DB, tokenID, action string) ext.AuditEvent {
	t.Helper()
	var event ext.AuditEvent
	if err := db.QueryRow(`
		SELECT event_id, occurred_at, action, user_id, email, resource, resource_id, detail
		FROM audit_outbox
		WHERE resource_id = ? AND action = ?`,
		tokenID, action,
	).Scan(
		&event.EventID, &event.OccurredAt, &event.Action, &event.UserID,
		&event.Email, &event.Resource, &event.ResourceID, &event.Detail,
	); err != nil {
		t.Fatalf("read audit event for %s: %v", tokenID, err)
	}
	event.OccurredAt = event.OccurredAt.UTC()
	return event
}

func assertExactOpAMPTokenAuditEvent(t *testing.T, event ext.AuditEvent, token models.OpAMPToken, action, userID, email string, occurredAt time.Time) {
	t.Helper()
	if event.EventID == "" || event.Action != action || event.Resource != "opamp_token" || event.ResourceID != token.ID ||
		event.UserID != userID || event.Email != email || event.Detail != "" || !event.OccurredAt.Equal(occurredAt.Truncate(time.Microsecond)) {
		t.Fatalf("audit event = %+v, want exact %s event for token %+v", event, action, token)
	}
}

func countOpAMPTokenAuditEvents(t *testing.T, db *store.DB, tokenID, action string) int {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM audit_outbox WHERE resource_id = ? AND action = ?`, tokenID, action).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func installRejectOpAMPTokenAuditTrigger(t *testing.T, db *store.DB) {
	t.Helper()
	if _, err := db.ExecPostgres(`
		CREATE FUNCTION reject_opamp_token_audit() RETURNS trigger
		LANGUAGE plpgsql AS $$
		BEGIN
			IF NEW.resource = 'opamp_token' THEN
				RAISE EXCEPTION 'forced audit outbox failure';
			END IF;
			RETURN NEW;
		END;
		$$;
		CREATE TRIGGER reject_opamp_token_audit
		BEFORE INSERT ON audit_outbox
		FOR EACH ROW EXECUTE FUNCTION reject_opamp_token_audit();
	`); err != nil {
		t.Fatalf("install rejecting audit trigger: %v", err)
	}
}

func TestOpAMPTokenRawSecretHashesToStoredDigest(t *testing.T) {
	db, router, _ := newOpAMPTokenTestAPI(t, nil, nil)
	rec := serveOpAMPTokenRequest(t, router, http.MethodPost, "/api/v1/opamp/tokens", `{"name":"hash-check"}`, []string{"administrator"})
	var response struct {
		Token models.OpAMPToken `json:"token"`
		Value string            `json:"value"`
	}
	decodeOpAMPTokenResponse(t, rec, &response)
	id, digest, err := opampauth.ParseAndHash(response.Value)
	if err != nil || id != response.Token.ID {
		t.Fatalf("parse returned (%q, %v), want token ID %q", id, err, response.Token.ID)
	}
	var stored []byte
	if err := db.QueryRow(`SELECT secret_hash FROM opamp_tokens WHERE id = ?`, response.Token.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if len(stored) != sha256.Size || string(stored) != string(digest[:]) {
		t.Fatal("stored digest does not match the one-way hash of the returned secret")
	}
}
