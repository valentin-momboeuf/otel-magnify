# Extensibility & Module Overlay Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the community repo extensible so the enterprise repo can compose an enriched binary via Go module overlay, without modifying community code.

**Architecture:** Define extension interfaces in `pkg/ext/`, refactor all internal packages to accept these interfaces instead of concrete types, and expose a composable server builder in `pkg/server/`. The enterprise repo imports `pkg/server`, `pkg/ext`, and `pkg/models` to compose its binary with custom implementations.

**Tech Stack:** Go 1.25, chi router, opamp-go, SQLite (modernc.org/sqlite), goose migrations

---

## File Map

### New files

| File | Responsibility |
|------|---------------|
| `backend/pkg/ext/store.go` | Store interface (all data access methods) |
| `backend/pkg/ext/auth.go` | AuthProvider interface + UserInfo type + context helpers |
| `backend/pkg/ext/notifier.go` | AlertNotifier interface |
| `backend/pkg/ext/audit.go` | AuditLogger interface + no-op default |
| `backend/pkg/ext/ext_test.go` | Compile-time interface satisfaction checks |
| `backend/pkg/server/server.go` | Server builder + Run() |
| `backend/pkg/server/options.go` | Functional options |
| `backend/pkg/server/server_test.go` | Builder integration test |

### Modified files

| File | Change |
|------|--------|
| `backend/internal/auth/auth.go` | Add `ValidateTokenInfo()` method, update `Middleware` to store `UserInfo` in context |
| `backend/internal/api/router.go` | Change `API` struct to use `ext.Store`, `ext.AuthProvider`; update `NewRouter` signature |
| `backend/internal/api/authhandler.go` | Use `ext.Store` for user lookup, `ext.AuthProvider` for token gen |
| `backend/internal/api/agents.go` | Use `ext.UserInfoFromContext` instead of `auth.ClaimsFromContext` |
| `backend/internal/api/configs.go` | Use `ext.UserInfoFromContext` instead of `auth.ClaimsFromContext` |
| `backend/internal/api/agents_test.go` | Update `newTestAPI` to pass auth adapter |
| `backend/internal/alerts/engine.go` | Change `Engine` to use `ext.Store`, `[]ext.AlertNotifier` |
| `backend/internal/opamp/server.go` | Change `Server` to use `ext.Store` |
| `backend/cmd/server/main.go` | Simplify to use `pkg/server.New().Run()` |

---

### Task 1: Define UserInfo and context helpers

**Files:**
- Create: `backend/pkg/ext/auth.go`

- [ ] **Step 1: Write the compile-time test**

Create `backend/pkg/ext/ext_test.go`:

```go
package ext_test

import (
	"context"
	"testing"

	"otel-magnify/pkg/ext"
)

func TestUserInfoContext_RoundTrip(t *testing.T) {
	info := &ext.UserInfo{UserID: "u1", Email: "a@b.com", Role: "admin"}
	ctx := ext.ContextWithUserInfo(context.Background(), info)
	got := ext.UserInfoFromContext(ctx)
	if got == nil || got.UserID != "u1" || got.Email != "a@b.com" || got.Role != "admin" {
		t.Fatalf("round-trip failed: got %+v", got)
	}
}

func TestUserInfoFromContext_EmptyContext(t *testing.T) {
	got := ext.UserInfoFromContext(context.Background())
	if got != nil {
		t.Fatalf("expected nil from empty context, got %+v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./pkg/ext/ -v -run TestUserInfo`
Expected: compilation error (package does not exist yet)

- [ ] **Step 3: Write the implementation**

Create `backend/pkg/ext/auth.go`:

```go
// Package ext defines the extension interfaces for the otel-magnify module overlay.
// Enterprise builds import this package to implement custom providers.
package ext

import (
	"context"
	"net/http"
)

// userInfoKey is an unexported type to avoid context key collisions.
type userInfoKey struct{}

// UserInfo holds the authenticated user's identity, extracted by an AuthProvider.
// This is the provider-agnostic alternative to JWT-specific Claims.
type UserInfo struct {
	UserID string
	Email  string
	Role   string
}

// AuthProvider abstracts authentication. The community default uses JWT;
// enterprise builds can substitute SSO/SAML/OIDC providers.
type AuthProvider interface {
	// GenerateToken creates a signed token for the given user attributes.
	GenerateToken(userID, email, role string) (string, error)

	// ValidateToken verifies a token string and returns the user info.
	ValidateToken(tokenStr string) (*UserInfo, error)

	// Middleware returns an HTTP middleware that enforces authentication
	// and stores UserInfo in the request context.
	Middleware(next http.Handler) http.Handler
}

// UserInfoFromContext retrieves the UserInfo stored by an AuthProvider's Middleware.
// Returns nil if no user info is present (e.g. unauthenticated route).
func UserInfoFromContext(ctx context.Context) *UserInfo {
	info, _ := ctx.Value(userInfoKey{}).(*UserInfo)
	return info
}

// ContextWithUserInfo returns a new context carrying the given UserInfo.
// Used in tests and internal composition.
func ContextWithUserInfo(ctx context.Context, info *UserInfo) context.Context {
	return context.WithValue(ctx, userInfoKey{}, info)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./pkg/ext/ -v -run TestUserInfo`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd backend && git add pkg/ext/auth.go pkg/ext/ext_test.go
git commit -m "feat(ext): add UserInfo type, AuthProvider interface and context helpers"
```

---

### Task 2: Define Store interface

**Files:**
- Create: `backend/pkg/ext/store.go`
- Modify: `backend/pkg/ext/ext_test.go`

- [ ] **Step 1: Write the compile-time satisfaction check**

Append to `backend/pkg/ext/ext_test.go`:

```go
import "otel-magnify/internal/store"

// Compile-time check: store.DB must satisfy ext.Store.
var _ ext.Store = (*store.DB)(nil)
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./pkg/ext/ -v`
Expected: compilation error (ext.Store not defined)

- [ ] **Step 3: Write the implementation**

Create `backend/pkg/ext/store.go`:

```go
package ext

import "otel-magnify/pkg/models"

// Store abstracts all data access. The community default is internal/store.DB
// (SQLite or PostgreSQL). Enterprise builds can wrap or replace this to add
// tenant filtering, audit decoration, or alternative backends.
type Store interface {
	// Users
	CreateUser(u models.User) error
	GetUserByEmail(email string) (models.User, error)

	// Agents
	UpsertAgent(a models.Agent) error
	GetAgent(id string) (models.Agent, error)
	ListAgents() ([]models.Agent, error)
	UpdateAgentStatus(id, status string) error

	// Configs
	CreateConfig(c models.Config) error
	GetConfig(id string) (models.Config, error)
	ListConfigs() ([]models.Config, error)
	RecordAgentConfig(ac models.AgentConfig) error
	UpdateAgentConfigStatus(agentID, configID, status, errorMessage string) error
	GetLatestPendingAgentConfig(agentID string) (*models.AgentConfig, error)
	GetAgentConfigHistory(agentID string) ([]models.AgentConfig, error)
	GetLastAppliedAgentConfig(agentID string) (*models.AgentConfig, error)

	// Alerts
	CreateAlert(a models.Alert) error
	ResolveAlert(id string) error
	ListAlerts(includeResolved bool) ([]models.Alert, error)
	GetUnresolvedAlertByAgentAndRule(agentID, rule string) (*models.Alert, error)

	// Lifecycle
	Close() error
	Migrate() error
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./pkg/ext/ -v`
Expected: PASS (store.DB satisfies Store implicitly)

- [ ] **Step 5: Commit**

```bash
cd backend && git add pkg/ext/store.go pkg/ext/ext_test.go
git commit -m "feat(ext): add Store interface covering all data access methods"
```

---

### Task 3: Define AlertNotifier and AuditLogger interfaces

**Files:**
- Create: `backend/pkg/ext/notifier.go`
- Create: `backend/pkg/ext/audit.go`

- [ ] **Step 1: Write the compile-time check for WebhookNotifier**

Append to `backend/pkg/ext/ext_test.go`:

```go
import "otel-magnify/internal/alerts"

// Compile-time check: alerts.WebhookNotifier must satisfy ext.AlertNotifier.
var _ ext.AlertNotifier = (*alerts.WebhookNotifier)(nil)
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./pkg/ext/ -v`
Expected: compilation error (ext.AlertNotifier not defined)

- [ ] **Step 3: Write AlertNotifier interface**

Create `backend/pkg/ext/notifier.go`:

```go
package ext

import "otel-magnify/pkg/models"

// AlertNotifier sends alert notifications to an external system.
// The community default is WebhookNotifier; enterprise adds email, Slack, PagerDuty.
type AlertNotifier interface {
	Send(alert models.Alert)
}
```

- [ ] **Step 4: Write AuditLogger interface**

Create `backend/pkg/ext/audit.go`:

```go
package ext

import "context"

// AuditEvent represents an auditable action in the system.
type AuditEvent struct {
	Action   string // "config.push", "auth.login", "alert.resolve", etc.
	UserID   string
	Email    string
	Resource string // resource type: "agent", "config", "alert"
	ResourceID string
	Detail   string // optional human-readable detail
}

// AuditLogger records audit events. The community default is a no-op.
// Enterprise builds provide a persistent audit log.
type AuditLogger interface {
	Log(ctx context.Context, event AuditEvent)
}

// NopAuditLogger is the default no-op audit logger for the community tier.
type NopAuditLogger struct{}

func (NopAuditLogger) Log(context.Context, AuditEvent) {}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd backend && go test ./pkg/ext/ -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
cd backend && git add pkg/ext/notifier.go pkg/ext/audit.go pkg/ext/ext_test.go
git commit -m "feat(ext): add AlertNotifier and AuditLogger interfaces"
```

---

### Task 4: Update internal/auth to support AuthProvider interface

**Files:**
- Modify: `backend/internal/auth/auth.go`
- Modify: `backend/internal/auth/auth_test.go`
- Modify: `backend/pkg/ext/ext_test.go`

- [ ] **Step 1: Add compile-time check**

Append to `backend/pkg/ext/ext_test.go`:

```go
import "otel-magnify/internal/auth"

// Compile-time check: auth.Auth must satisfy ext.AuthProvider.
var _ ext.AuthProvider = (*auth.Auth)(nil)
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./pkg/ext/ -v`
Expected: compilation error (auth.Auth.ValidateToken returns *Claims, not *UserInfo)

- [ ] **Step 3: Update auth.Auth to satisfy AuthProvider**

In `backend/internal/auth/auth.go`, modify `ValidateToken` to return `*ext.UserInfo` and update `Middleware` to store `UserInfo` in context via `ext.ContextWithUserInfo`:

```go
package auth

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"otel-magnify/pkg/ext"
)

// claims holds the JWT payload. Internal to this package; the public API
// uses ext.UserInfo to stay provider-agnostic.
type claims struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
	Role   string `json:"role"`
	jwt.RegisteredClaims
}

// Auth handles token generation and validation for a given HMAC secret.
type Auth struct {
	secret []byte
}

// New creates an Auth instance. The secret must be at least 32 bytes for
// adequate HMAC-SHA256 security.
func New(secret string) *Auth {
	return &Auth{secret: []byte(secret)}
}

// GenerateToken mints a signed JWT for the given user attributes.
// Tokens expire after 24 hours.
func (a *Auth) GenerateToken(userID, email, role string) (string, error) {
	c := claims{
		UserID: userID,
		Email:  email,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, c)
	return token.SignedString(a.secret)
}

// ValidateToken parses and verifies the token string, returning the user info.
// Rejects tokens signed with a non-HMAC algorithm to prevent the "alg:none" attack.
func (a *Auth) ValidateToken(tokenStr string) (*ext.UserInfo, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &claims{}, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return a.secret, nil
	})
	if err != nil {
		return nil, err
	}
	c, ok := token.Claims.(*claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}
	return &ext.UserInfo{
		UserID: c.UserID,
		Email:  c.Email,
		Role:   c.Role,
	}, nil
}

// Middleware returns an HTTP handler that enforces Bearer token authentication.
// On success it stores the validated UserInfo in the request context.
func (a *Auth) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if header == "" || !strings.HasPrefix(header, "Bearer ") {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		tokenStr := strings.TrimPrefix(header, "Bearer ")
		info, err := a.ValidateToken(tokenStr)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		ctx := ext.ContextWithUserInfo(r.Context(), info)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
```

- [ ] **Step 4: Update auth_test.go**

In `backend/internal/auth/auth_test.go`, update all references from `Claims` to `ext.UserInfo`. Replace `claims.UserID` with `info.UserID`, `claims.Email` with `info.Email`, etc. Remove any references to `ClaimsFromContext` and use `ext.UserInfoFromContext` instead. Remove the old `Claims` and `ClaimsFromContext` exports — they no longer exist.

Key changes:
- `auth.ValidateToken()` now returns `*ext.UserInfo` instead of `*auth.Claims`
- Replace `auth.ClaimsFromContext()` with `ext.UserInfoFromContext()`
- Replace `auth.ContextWithClaims()` with `ext.ContextWithUserInfo()`

- [ ] **Step 5: Run auth tests**

Run: `cd backend && go test ./internal/auth/ -v`
Expected: PASS

- [ ] **Step 6: Run ext compile-time checks**

Run: `cd backend && go test ./pkg/ext/ -v`
Expected: PASS (auth.Auth now satisfies ext.AuthProvider)

- [ ] **Step 7: Commit**

```bash
cd backend && git add internal/auth/auth.go internal/auth/auth_test.go pkg/ext/ext_test.go
git commit -m "refactor(auth): implement ext.AuthProvider interface, return UserInfo instead of Claims"
```

---

### Task 5: Refactor internal/api to use ext interfaces

**Files:**
- Modify: `backend/internal/api/router.go`
- Modify: `backend/internal/api/agents.go`
- Modify: `backend/internal/api/configs.go`
- Modify: `backend/internal/api/authhandler.go`
- Modify: `backend/internal/api/agents_test.go`

- [ ] **Step 1: Update API struct and NewRouter signature**

In `backend/internal/api/router.go`:

Change the `API` struct and `NewRouter` to accept interfaces:

```go
import (
	"otel-magnify/pkg/ext"
	// remove "otel-magnify/internal/auth"
	// remove "otel-magnify/internal/store"
)

type API struct {
	db    ext.Store
	auth  ext.AuthProvider
	hub   *Hub
	opamp OpAMPPusher
}

func NewRouter(db ext.Store, a ext.AuthProvider, hub *Hub, opampSrv OpAMPPusher, corsOrigins string, staticFS fs.FS) http.Handler {
	api := &API{db: db, auth: a, hub: hub, opamp: opampSrv}
	// ... rest unchanged ...

	// Protected routes: use a.Middleware (interface method, same signature)
	r.Group(func(r chi.Router) {
		r.Use(a.Middleware)
		// ... routes unchanged ...

		// WebSocket: use a.ValidateToken (now returns *ext.UserInfo)
		r.Get("/ws", func(w http.ResponseWriter, r *http.Request) {
			token := r.URL.Query().Get("token")
			if token == "" {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			if _, err := a.ValidateToken(token); err != nil {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			hub.HandleWS(w, r)
		})
	})
	// ... rest unchanged ...
}
```

- [ ] **Step 2: Update agents.go to use ext.UserInfoFromContext**

In `backend/internal/api/agents.go`, replace:

```go
import "otel-magnify/internal/auth"
// ...
if claims := auth.ClaimsFromContext(r.Context()); claims != nil {
    pushedBy = claims.Email
}
```

With:

```go
import "otel-magnify/pkg/ext"
// ...
if info := ext.UserInfoFromContext(r.Context()); info != nil {
    pushedBy = info.Email
}
```

- [ ] **Step 3: Update configs.go to use ext.UserInfoFromContext**

In `backend/internal/api/configs.go`, replace:

```go
import "otel-magnify/internal/auth"
// ...
claims := auth.ClaimsFromContext(r.Context())
if claims != nil {
    createdBy = claims.Email
}
```

With:

```go
import "otel-magnify/pkg/ext"
// ...
info := ext.UserInfoFromContext(r.Context())
if info != nil {
    createdBy = info.Email
}
```

- [ ] **Step 4: Update authhandler.go**

In `backend/internal/api/authhandler.go`, no changes needed to the handler logic — `a.db.GetUserByEmail` and `a.auth.GenerateToken` are method calls that work the same on the interface. Just verify the imports are correct (no `store` or `auth` import needed).

- [ ] **Step 5: Update agents_test.go**

In `backend/internal/api/agents_test.go`, update:

```go
import (
	"otel-magnify/internal/auth"
	"otel-magnify/internal/store"
	"otel-magnify/pkg/ext"
)

func newTestAPI(t *testing.T) (ext.Store, http.Handler, *fakeOpAMP) {
	t.Helper()
	db, err := store.Open("sqlite", ":memory:")
	// ... same setup ...
	// Return type is ext.Store (store.DB satisfies it)
	return db, router, opampFake
}

func authedRequest(t *testing.T, method, url string) *http.Request {
	t.Helper()
	a := auth.New("test-secret-key-at-least-32-bytes!")
	token, _ := a.GenerateToken("user-001", "admin@test.com", "admin")
	req := httptest.NewRequest(method, url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}
```

Note: `newTestAPI` now returns `ext.Store` instead of `*store.DB`. Tests that call `db.UpsertAgent(...)` etc. still work because `ext.Store` has those methods.

- [ ] **Step 6: Run all API tests**

Run: `cd backend && go test ./internal/api/ -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
cd backend && git add internal/api/router.go internal/api/agents.go internal/api/configs.go internal/api/authhandler.go internal/api/agents_test.go
git commit -m "refactor(api): accept ext.Store and ext.AuthProvider interfaces instead of concrete types"
```

---

### Task 6: Refactor internal/alerts to use ext interfaces

**Files:**
- Modify: `backend/internal/alerts/engine.go`
- Modify: `backend/internal/alerts/engine_test.go`

- [ ] **Step 1: Define local AlertStore interface and update Engine**

In `backend/internal/alerts/engine.go`, replace the concrete `*store.DB` with an interface and accept `[]ext.AlertNotifier`:

```go
package alerts

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"time"

	"otel-magnify/pkg/ext"
	"otel-magnify/pkg/models"
)

// Broadcaster pushes real-time updates to connected frontends.
type Broadcaster interface {
	BroadcastAlertUpdate(alert models.Alert)
}

// AlertStore is the subset of ext.Store needed by the alert engine.
type AlertStore interface {
	ListAgents() ([]models.Agent, error)
	GetUnresolvedAlertByAgentAndRule(agentID, rule string) (*models.Alert, error)
	CreateAlert(a models.Alert) error
	ResolveAlert(id string) error
	GetLatestPendingAgentConfig(agentID string) (*models.AgentConfig, error)
}

type Engine struct {
	db          AlertStore
	hub         Broadcaster
	downTimeout time.Duration
	minVersion  string
	notifiers   []ext.AlertNotifier
}

func New(db AlertStore, hub Broadcaster, downTimeout time.Duration, minVersion string, notifiers ...ext.AlertNotifier) *Engine {
	return &Engine{
		db:          db,
		hub:         hub,
		downTimeout: downTimeout,
		minVersion:  minVersion,
		notifiers:   notifiers,
	}
}
```

Update every alert fire site to call all notifiers:

```go
// Replace:
//   if e.webhook != nil { go e.webhook.Send(alert) }
// With:
for _, n := range e.notifiers {
    n := n
    go n.Send(alert)
}
```

Apply this replacement in `evaluateAgentDown`, `evaluateConfigDrift`, and `evaluateVersionOutdated`.

- [ ] **Step 2: Update engine_test.go**

Update the test to pass `AlertStore` interface and notifiers. The existing test likely creates a `*store.DB` — it still satisfies `AlertStore` implicitly, so minimal changes needed. Update the `New()` call signature (remove `webhookURL` string, pass notifiers instead). If the test used `api.Hub` directly, switch to a mock `Broadcaster`.

- [ ] **Step 3: Run tests**

Run: `cd backend && go test ./internal/alerts/ -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
cd backend && git add internal/alerts/engine.go internal/alerts/engine_test.go
git commit -m "refactor(alerts): accept AlertStore interface and ext.AlertNotifier slice"
```

---

### Task 7: Refactor internal/opamp to use ext.Store

**Files:**
- Modify: `backend/internal/opamp/server.go`
- Modify: `backend/internal/opamp/rollback.go`
- Modify: `backend/internal/opamp/server_test.go`

- [ ] **Step 1: Define local OpAMPStore interface**

In `backend/internal/opamp/server.go`, replace `*store.DB` with a local interface:

```go
// OpAMPStore is the subset of ext.Store needed by the OpAMP server.
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
```

Update the `Server` struct:

```go
type Server struct {
	opamp    opampServer.OpAMPServer
	store    OpAMPStore   // was *store.DB
	notifier Notifier
	// ... rest unchanged
}

func New(db OpAMPStore, notifier Notifier) *Server {
	// ... same, db is now OpAMPStore
}
```

Remove the `import "otel-magnify/internal/store"` line.

- [ ] **Step 2: Update rollback.go**

In `backend/internal/opamp/rollback.go`, remove the `store` import if present. The `s.store` field is now the `OpAMPStore` interface — same methods, no changes to logic.

- [ ] **Step 3: Update server_test.go**

Update test setup: if it creates `*store.DB`, it still satisfies `OpAMPStore`. Change the `New()` call if needed. Remove `import "otel-magnify/internal/store"` if the test uses a fake — if it uses real `store.DB`, keep the import.

- [ ] **Step 4: Run tests**

Run: `cd backend && go test ./internal/opamp/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd backend && git add internal/opamp/server.go internal/opamp/rollback.go internal/opamp/server_test.go
git commit -m "refactor(opamp): accept OpAMPStore interface instead of concrete *store.DB"
```

---

### Task 8: Create pkg/server builder

**Files:**
- Create: `backend/pkg/server/options.go`
- Create: `backend/pkg/server/server.go`
- Create: `backend/pkg/server/server_test.go`

- [ ] **Step 1: Write the builder test**

Create `backend/pkg/server/server_test.go`:

```go
package server_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"otel-magnify/internal/auth"
	"otel-magnify/internal/store"
	"otel-magnify/pkg/server"
)

func TestNew_DefaultsCompile(t *testing.T) {
	db, err := store.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}

	a := auth.New("test-secret-key-at-least-32-bytes!")

	srv := server.New(server.Config{
		ListenAddr: ":0",
		OpAMPAddr:  ":0",
	}, db, a)

	if srv == nil {
		t.Fatal("New returned nil")
	}
}

func TestServer_StartsAndStops(t *testing.T) {
	db, err := store.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}

	a := auth.New("test-secret-key-at-least-32-bytes!")

	srv := server.New(server.Config{
		ListenAddr: ":0",
		OpAMPAddr:  ":0",
	}, db, a)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Run(ctx) }()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)
	cancel()

	if err := <-errCh; err != nil && err != context.Canceled {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./pkg/server/ -v`
Expected: compilation error (package does not exist)

- [ ] **Step 3: Write options.go**

Create `backend/pkg/server/options.go`:

```go
package server

import (
	"io/fs"

	"github.com/go-chi/chi/v5"

	"otel-magnify/pkg/ext"
)

// Config holds the server's listen addresses and feature flags.
type Config struct {
	ListenAddr      string // default ":8080"
	OpAMPAddr       string // default ":4320"
	CORSOrigins     string
	MinAgentVersion string
}

// Option configures optional features on the Server.
type Option func(*Server)

// WithNotifier adds an AlertNotifier (webhook, email, Slack, etc.).
// Multiple notifiers can be added; all are called on alert fire.
func WithNotifier(n ext.AlertNotifier) Option {
	return func(s *Server) {
		s.notifiers = append(s.notifiers, n)
	}
}

// WithAuditLogger sets the audit logger. Default is ext.NopAuditLogger.
func WithAuditLogger(l ext.AuditLogger) Option {
	return func(s *Server) {
		s.auditLogger = l
	}
}

// WithStaticFS sets the embedded frontend assets for SPA serving.
func WithStaticFS(fsys fs.FS) Option {
	return func(s *Server) {
		s.staticFS = fsys
	}
}

// WithRouterHook adds a function that can modify the chi router before
// the server starts. Use this to add middleware (RBAC, audit) or extra routes.
func WithRouterHook(fn func(chi.Router)) Option {
	return func(s *Server) {
		s.routerHooks = append(s.routerHooks, fn)
	}
}
```

- [ ] **Step 4: Write server.go**

Create `backend/pkg/server/server.go`:

```go
// Package server provides a composable server builder for otel-magnify.
// Community and enterprise builds both use this to start the server with
// different providers.
package server

import (
	"context"
	"io/fs"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"otel-magnify/internal/alerts"
	"otel-magnify/internal/api"
	"otel-magnify/internal/opamp"
	"otel-magnify/pkg/ext"
)

// Server composes the otel-magnify subsystems.
type Server struct {
	cfg         Config
	store       ext.Store
	auth        ext.AuthProvider
	notifiers   []ext.AlertNotifier
	auditLogger ext.AuditLogger
	staticFS    fs.FS
	routerHooks []func(chi.Router)
}

// New creates a Server with the given store, auth provider, and options.
func New(cfg Config, store ext.Store, auth ext.AuthProvider, opts ...Option) *Server {
	s := &Server{
		cfg:         cfg,
		store:       store,
		auth:        auth,
		auditLogger: ext.NopAuditLogger{},
	}
	for _, opt := range opts {
		opt(s)
	}
	if s.cfg.ListenAddr == "" {
		s.cfg.ListenAddr = ":8080"
	}
	if s.cfg.OpAMPAddr == "" {
		s.cfg.OpAMPAddr = ":4320"
	}
	return s
}

// Run starts all subsystems and blocks until ctx is cancelled.
// Returns nil on clean shutdown.
func (s *Server) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// WebSocket hub
	hub := api.NewHub()
	go hub.Run()

	// OpAMP server
	opampSrv := opamp.New(s.store, hub)
	opampHandler, connCtx, err := opampSrv.Attach()
	if err != nil {
		return err
	}

	opampMux := http.NewServeMux()
	opampMux.HandleFunc("/v1/opamp", opampHandler)

	opampListener, err := net.Listen("tcp", s.cfg.OpAMPAddr)
	if err != nil {
		return err
	}
	opampHTTP := &http.Server{
		Handler:     opampMux,
		ConnContext: connCtx,
	}
	go func() {
		log.Printf("OpAMP server listening on %s", opampListener.Addr())
		if err := opampHTTP.Serve(opampListener); err != nil && err != http.ErrServerClosed {
			log.Printf("OpAMP server: %v", err)
		}
	}()

	// Alert engine
	alertEngine := alerts.New(s.store, hub, 5*time.Minute, s.cfg.MinAgentVersion, s.notifiers...)
	go alertEngine.Start(ctx, 30*time.Second)
	log.Println("Alert engine started (30s interval)")

	// REST API
	router := api.NewRouter(s.store, s.auth, hub, opampSrv, s.cfg.CORSOrigins, s.staticFS)

	// Apply router hooks (enterprise can add RBAC middleware, extra routes, etc.)
	if len(s.routerHooks) > 0 {
		if chiRouter, ok := router.(chi.Router); ok {
			for _, hook := range s.routerHooks {
				hook(chiRouter)
			}
		}
	}

	apiListener, err := net.Listen("tcp", s.cfg.ListenAddr)
	if err != nil {
		return err
	}
	apiHTTP := &http.Server{
		Handler: router,
	}
	go func() {
		log.Printf("API server listening on %s", apiListener.Addr())
		if err := apiHTTP.Serve(apiListener); err != nil && err != http.ErrServerClosed {
			log.Printf("API server: %v", err)
		}
	}()

	// Block until context cancellation
	<-ctx.Done()
	log.Println("Shutting down...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	apiHTTP.Shutdown(shutdownCtx)
	opampHTTP.Shutdown(shutdownCtx)
	opampSrv.Stop(shutdownCtx)
	hub.Stop()
	log.Println("Shutdown complete")

	return nil
}
```

- [ ] **Step 5: Run tests**

Run: `cd backend && go test ./pkg/server/ -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
cd backend && git add pkg/server/server.go pkg/server/options.go pkg/server/server_test.go
git commit -m "feat(server): add composable server builder with functional options"
```

---

### Task 9: Refactor cmd/server/main.go

**Files:**
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 1: Rewrite main.go to use pkg/server**

```go
package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/crypto/bcrypt"

	"otel-magnify/internal/alerts"
	"otel-magnify/internal/auth"
	"otel-magnify/internal/config"
	"otel-magnify/internal/store"
	"otel-magnify/pkg/ext"
	"otel-magnify/pkg/models"
	"otel-magnify/pkg/server"
)

//go:embed dist
var frontendDist embed.FS

func main() {
	cfg := config.Load()
	if cfg.JWTSecret == "" {
		fmt.Fprintln(os.Stderr, "JWT_SECRET environment variable is required")
		os.Exit(1)
	}

	// Database
	db, err := store.Open(cfg.DBDriver, cfg.DBDSN)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}
	log.Println("Database migrations applied")

	seedAdmin(db)

	// Auth
	a := auth.New(cfg.JWTSecret)

	// Server options
	var opts []server.Option

	// Webhook notifier
	if cfg.WebhookURL != "" {
		opts = append(opts, server.WithNotifier(alerts.NewWebhookNotifier(cfg.WebhookURL)))
	}

	// Embedded frontend
	if sub, err := fs.Sub(frontendDist, "dist"); err == nil {
		opts = append(opts, server.WithStaticFS(sub))
	}

	srv := server.New(server.Config{
		ListenAddr:      cfg.ListenAddr,
		OpAMPAddr:       cfg.OpAMPAddr,
		CORSOrigins:     cfg.CORSOrigins,
		MinAgentVersion: cfg.MinAgentVersion,
	}, db, a, opts...)

	// Graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		cancel()
	}()

	if err := srv.Run(ctx); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

func seedAdmin(db ext.Store, ) {
	email := os.Getenv("SEED_ADMIN_EMAIL")
	password := os.Getenv("SEED_ADMIN_PASSWORD")
	if email == "" || password == "" {
		return
	}
	if _, err := db.GetUserByEmail(email); err == nil {
		log.Printf("Seed admin: user %s already exists, skipping", email)
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		log.Printf("Seed admin: failed to hash password: %v", err)
		return
	}
	user := models.User{
		ID:           "admin-seed-001",
		Email:        email,
		PasswordHash: string(hash),
		Role:         "admin",
	}
	if err := db.CreateUser(user); err != nil {
		log.Printf("Seed admin: failed to create user: %v", err)
		return
	}
	log.Printf("Seed admin: created user %s", email)
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd backend && go build ./cmd/server/`
Expected: build succeeds

Note: the build requires the `dist/` directory (embedded frontend). If not present, create a placeholder: `mkdir -p cmd/server/dist && touch cmd/server/dist/.gitkeep`

- [ ] **Step 3: Commit**

```bash
cd backend && git add cmd/server/main.go
git commit -m "refactor(main): use pkg/server builder for composable startup"
```

---

### Task 10: Full test suite verification

**Files:** none (verification only)

- [ ] **Step 1: Run all backend tests**

Run: `cd backend && go test ./... -count=1`
Expected: all tests PASS

- [ ] **Step 2: Run go vet**

Run: `cd backend && go vet ./...`
Expected: no issues

- [ ] **Step 3: Verify build**

Run: `cd backend && go build ./cmd/server/`
Expected: build succeeds

- [ ] **Step 4: Final commit if any fixes were needed**

```bash
git add -A && git commit -m "fix: resolve test issues from extensibility refactor"
```

Only run this if Step 1-3 required fixes.

---

## What this enables for the enterprise repo

After this plan is complete, the enterprise repo (`otel-magnify-enterprise`) can compose its binary like this:

```go
// otel-magnify-enterprise/cmd/server/main.go
package main

import (
	"github.com/valentin-momboeuf/otel-magnify/pkg/ext"
	"github.com/valentin-momboeuf/otel-magnify/pkg/server"

	"otel-magnify-enterprise/internal/license"
	"otel-magnify-enterprise/internal/sso"
	"otel-magnify-enterprise/internal/audit"
	// ...
)

func main() {
	lic := license.LoadFile("magnify.lic")

	var auth ext.AuthProvider
	if lic.HasFeature("sso") {
		auth = sso.NewProvider(...)
	} else {
		auth = jwtauth.New(cfg.JWTSecret)
	}

	opts := []server.Option{
		server.WithAuditLogger(audit.New(db)),
	}
	if lic.HasFeature("rbac") {
		opts = append(opts, server.WithRouterHook(rbac.Middleware(...)))
	}

	srv := server.New(serverCfg, db, auth, opts...)
	srv.Run(ctx)
}
```

No community code is modified. The enterprise binary is a thin composition layer.
