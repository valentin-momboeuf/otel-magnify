# otel-magnify Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a web application for centralized OpenTelemetry agent management via OpAMP, supporting Collectors and SDK agents with real-time status, config push, and alerting.

**Architecture:** Go backend exposes an OpAMP WebSocket endpoint for agents (via `opamp-go` Attach) and a REST+WebSocket API for the React frontend (via `chi`). Agents register on connect, heartbeats update state in SQLite/Postgres, and an alert engine evaluates rules every 30s. Frontend receives real-time updates via WebSocket fan-out from a hub.

**Tech Stack:** Go 1.22+, `open-telemetry/opamp-go`, `go-chi/chi/v5`, `pressly/goose/v3`, `golang-jwt/jwt/v5`, `modernc.org/sqlite`, `jackc/pgx/v5/stdlib`, `gorilla/websocket` — React 18 + TypeScript, Vite 5, Zustand, TanStack Query v5, Recharts, CodeMirror 6, React Router v6

---

## File Map

### Backend

| File | Responsibility |
|---|---|
| `backend/cmd/server/main.go` | Entrypoint: load config, wire deps, start server |
| `backend/internal/config/config.go` | App config from env vars |
| `backend/pkg/models/models.go` | Shared structs: Agent, Config, Alert, User |
| `backend/internal/store/db.go` | DB open + goose migration runner |
| `backend/internal/store/db_test.go` | DB init + migration tests |
| `backend/internal/store/migrations/*.sql` | SQL migration files |
| `backend/internal/store/agents.go` | Agent CRUD |
| `backend/internal/store/agents_test.go` | Agent CRUD tests |
| `backend/internal/store/configs.go` | Config versioned storage |
| `backend/internal/store/configs_test.go` | Config storage tests |
| `backend/internal/store/alerts.go` | Alert create/resolve/list |
| `backend/internal/store/alerts_test.go` | Alert storage tests |
| `backend/internal/store/users.go` | User create/authenticate |
| `backend/internal/store/users_test.go` | User storage tests |
| `backend/internal/opamp/server.go` | OpAMP server: agent registry, heartbeat processing, config push |
| `backend/internal/opamp/server_test.go` | OpAMP integration tests |
| `backend/internal/auth/auth.go` | JWT generation + validation middleware |
| `backend/internal/auth/auth_test.go` | JWT tests |
| `backend/internal/api/wshub.go` | WebSocket hub: fan-out to frontend clients |
| `backend/internal/api/wshub_test.go` | WS hub tests |
| `backend/internal/api/router.go` | Chi router assembly |
| `backend/internal/api/agents.go` | REST handlers: agents |
| `backend/internal/api/agents_test.go` | Agent handler tests |
| `backend/internal/api/configs.go` | REST handlers: configs |
| `backend/internal/api/configs_test.go` | Config handler tests |
| `backend/internal/api/alerts.go` | REST handlers: alerts |
| `backend/internal/api/authhandler.go` | REST handlers: login |
| `backend/internal/alerts/engine.go` | Alert rule evaluation loop |
| `backend/internal/alerts/engine_test.go` | Alert engine tests |

### Frontend

| File | Responsibility |
|---|---|
| `frontend/src/main.tsx` | React entrypoint |
| `frontend/src/App.tsx` | Router setup + QueryClient |
| `frontend/src/api/client.ts` | Axios instance + typed API calls |
| `frontend/src/api/websocket.ts` | WebSocket client + reconnect |
| `frontend/src/store/index.ts` | Zustand store |
| `frontend/src/components/layout/Layout.tsx` | Shell: navbar + sidebar + outlet |
| `frontend/src/components/agents/StatusBadge.tsx` | Agent status indicator |
| `frontend/src/components/agents/AgentCard.tsx` | Agent summary card |
| `frontend/src/components/config/YamlEditor.tsx` | CodeMirror 6 YAML wrapper |
| `frontend/src/pages/Dashboard.tsx` | Overview: counts + recent alerts + chart |
| `frontend/src/pages/Agents.tsx` | Agent list with filters |
| `frontend/src/pages/AgentDetail.tsx` | Single agent detail |
| `frontend/src/pages/Configs.tsx` | Config template management |
| `frontend/src/pages/Alerts.tsx` | Alerts list + rules |
| `frontend/src/pages/Login.tsx` | Login form |

### Deployment

| File | Responsibility |
|---|---|
| `Dockerfile` | Multi-stage: frontend build → embed in Go binary |
| `docker-compose.yml` | Local stack: app + optional Postgres |
| `helm/otel-magnify/Chart.yaml` | Helm chart metadata |
| `helm/otel-magnify/values.yaml` | Default values |
| `helm/otel-magnify/templates/deployment.yaml` | K8s Deployment |
| `helm/otel-magnify/templates/service.yaml` | K8s Service |
| `helm/otel-magnify/templates/ingress.yaml` | K8s Ingress |
| `helm/otel-magnify/templates/secret.yaml` | K8s Secret for JWT_SECRET |

---

## Phase 1: Backend Foundation

### Task 1: Go module + project scaffold

**Files:**
- Create: `backend/go.mod`
- Create: `backend/cmd/server/main.go`
- Create: `backend/internal/config/config.go`

- [ ] **Step 1: Initialize Go module and directories**

```bash
cd backend
go mod init otel-magnify
mkdir -p cmd/server internal/{config,opamp,store/migrations,api,alerts,auth} pkg/models
```

- [ ] **Step 2: Write config loader**

```go
// backend/internal/config/config.go
package config

import "os"

type Config struct {
	DBDriver    string // "sqlite" or "pgx"
	DBDSN       string // file path for sqlite, connection string for postgres
	ListenAddr  string // e.g. ":8080"
	OpAMPAddr   string // e.g. ":4320"
	JWTSecret   string
	CORSOrigins string // comma-separated allowed origins
}

func Load() Config {
	return Config{
		DBDriver:    getenv("DB_DRIVER", "sqlite"),
		DBDSN:       getenv("DB_DSN", "otel-magnify.db"),
		ListenAddr:  getenv("LISTEN_ADDR", ":8080"),
		OpAMPAddr:   getenv("OPAMP_ADDR", ":4320"),
		JWTSecret:   getenv("JWT_SECRET", ""),
		CORSOrigins: getenv("CORS_ORIGINS", "http://localhost:5173"),
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
```

- [ ] **Step 3: Write stub main.go**

```go
// backend/cmd/server/main.go
package main

import (
	"fmt"
	"os"

	"otel-magnify/internal/config"
)

func main() {
	cfg := config.Load()
	if cfg.JWTSecret == "" {
		fmt.Fprintln(os.Stderr, "JWT_SECRET is required")
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "otel-magnify starting on %s\n", cfg.ListenAddr)
}
```

- [ ] **Step 4: Verify compilation**

Run: `cd backend && go build ./cmd/server/`
Expected: no errors, binary created.

- [ ] **Step 5: Commit**

```bash
git add backend/
git commit -m "feat: initialize Go backend module with config"
```

---

### Task 2: Shared models

**Files:**
- Create: `backend/pkg/models/models.go`

- [ ] **Step 1: Write all model types**

```go
// backend/pkg/models/models.go
package models

import (
	"database/sql"
	"encoding/json"
	"time"
)

// Labels is a map[string]string stored as JSON TEXT in the DB.
type Labels map[string]string

func (l Labels) Value() (string, error) {
	b, err := json.Marshal(l)
	return string(b), err
}

func (l *Labels) Scan(src any) error {
	switch v := src.(type) {
	case string:
		return json.Unmarshal([]byte(v), l)
	case []byte:
		return json.Unmarshal(v, l)
	case nil:
		*l = make(Labels)
		return nil
	default:
		return json.Unmarshal([]byte("{}"), l)
	}
}

type Agent struct {
	ID             string         `json:"id"`
	DisplayName    string         `json:"display_name"`
	Type           string         `json:"type"`    // "collector" | "sdk"
	Version        string         `json:"version"`
	Status         string         `json:"status"`  // "connected" | "disconnected" | "degraded"
	LastSeenAt     time.Time      `json:"last_seen_at"`
	Labels         Labels         `json:"labels"`
	ActiveConfigID sql.NullString `json:"active_config_id,omitempty"`
}

type Config struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
	CreatedBy string    `json:"created_by"`
}

type AgentConfig struct {
	AgentID   string    `json:"agent_id"`
	ConfigID  string    `json:"config_id"`
	AppliedAt time.Time `json:"applied_at"`
	Status    string    `json:"status"` // "pending" | "applied" | "failed"
}

type Alert struct {
	ID         string     `json:"id"`
	AgentID    string     `json:"agent_id"`
	Rule       string     `json:"rule"`     // "agent_down" | "config_drift" | "version_outdated"
	Severity   string     `json:"severity"` // "warning" | "critical"
	Message    string     `json:"message"`
	FiredAt    time.Time  `json:"fired_at"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
}

type User struct {
	ID           string  `json:"id"`
	Email        string  `json:"email"`
	PasswordHash string  `json:"-"`
	Role         string  `json:"role"` // "admin" | "viewer"
	TenantID     *string `json:"tenant_id,omitempty"`
}
```

- [ ] **Step 2: Verify compilation**

Run: `cd backend && go build ./pkg/models/`
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add backend/pkg/
git commit -m "feat: add shared data models"
```

---

### Task 3: Database layer + migrations

**Files:**
- Create: `backend/internal/store/db.go`
- Create: `backend/internal/store/db_test.go`
- Create: `backend/internal/store/migrations/00001_create_configs.sql`
- Create: `backend/internal/store/migrations/00002_create_agents.sql`
- Create: `backend/internal/store/migrations/00003_create_agent_configs.sql`
- Create: `backend/internal/store/migrations/00004_create_alerts.sql`
- Create: `backend/internal/store/migrations/00005_create_users.sql`

- [ ] **Step 1: Add dependencies**

```bash
cd backend
go get github.com/pressly/goose/v3
go get modernc.org/sqlite
go get github.com/jackc/pgx/v5/stdlib
```

- [ ] **Step 2: Write migration SQL files**

```sql
-- backend/internal/store/migrations/00001_create_configs.sql
-- +goose Up
CREATE TABLE configs (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    content    TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT NOT NULL DEFAULT ''
);

-- +goose Down
DROP TABLE IF EXISTS configs;
```

```sql
-- backend/internal/store/migrations/00002_create_agents.sql
-- +goose Up
CREATE TABLE agents (
    id               TEXT PRIMARY KEY,
    display_name     TEXT NOT NULL DEFAULT '',
    type             TEXT NOT NULL CHECK (type IN ('collector', 'sdk')),
    version          TEXT NOT NULL DEFAULT '',
    status           TEXT NOT NULL DEFAULT 'disconnected' CHECK (status IN ('connected', 'disconnected', 'degraded')),
    last_seen_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    labels           TEXT NOT NULL DEFAULT '{}',
    active_config_id TEXT REFERENCES configs(id)
);

-- +goose Down
DROP TABLE IF EXISTS agents;
```

```sql
-- backend/internal/store/migrations/00003_create_agent_configs.sql
-- +goose Up
CREATE TABLE agent_configs (
    agent_id   TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    config_id  TEXT NOT NULL REFERENCES configs(id),
    applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    status     TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'applied', 'failed')),
    PRIMARY KEY (agent_id, config_id, applied_at)
);

-- +goose Down
DROP TABLE IF EXISTS agent_configs;
```

```sql
-- backend/internal/store/migrations/00004_create_alerts.sql
-- +goose Up
CREATE TABLE alerts (
    id          TEXT PRIMARY KEY,
    agent_id    TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    rule        TEXT NOT NULL,
    severity    TEXT NOT NULL CHECK (severity IN ('warning', 'critical')),
    message     TEXT NOT NULL,
    fired_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    resolved_at TIMESTAMP
);

-- +goose Down
DROP TABLE IF EXISTS alerts;
```

```sql
-- backend/internal/store/migrations/00005_create_users.sql
-- +goose Up
CREATE TABLE users (
    id            TEXT PRIMARY KEY,
    email         TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    role          TEXT NOT NULL DEFAULT 'viewer' CHECK (role IN ('admin', 'viewer')),
    tenant_id     TEXT
);

-- +goose Down
DROP TABLE IF EXISTS users;
```

- [ ] **Step 3: Write the failing test**

```go
// backend/internal/store/db_test.go
package store

import "testing"

func TestOpen_SQLite_InMemory(t *testing.T) {
	db, err := Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
}

func TestMigrate(t *testing.T) {
	db, err := Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// Verify tables exist by querying them
	tables := []string{"configs", "agents", "agent_configs", "alerts", "users"}
	for _, table := range tables {
		_, err := db.Exec("SELECT count(*) FROM " + table)
		if err != nil {
			t.Errorf("table %s not created: %v", table, err)
		}
	}
}
```

- [ ] **Step 4: Run test to verify it fails**

Run: `cd backend && go test ./internal/store/ -run TestOpen -v`
Expected: FAIL — `Open` not defined.

- [ ] **Step 5: Write db.go implementation**

```go
// backend/internal/store/db.go
package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

type DB struct {
	*sql.DB
	driver string
}

func Open(driver, dsn string) (*DB, error) {
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}
	if driver == "sqlite" {
		// Enable foreign keys and WAL mode for SQLite
		if _, err := db.Exec("PRAGMA foreign_keys = ON; PRAGMA journal_mode = WAL;"); err != nil {
			return nil, fmt.Errorf("sqlite pragmas: %w", err)
		}
	}
	return &DB{DB: db, driver: driver}, nil
}

func (d *DB) Migrate() error {
	fsys, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("migrations fs: %w", err)
	}

	dialect := goose.DialectSQLite3
	if d.driver == "pgx" {
		dialect = goose.DialectPostgres
	}

	provider, err := goose.NewProvider(dialect, d.DB, fsys)
	if err != nil {
		return fmt.Errorf("goose provider: %w", err)
	}

	if _, err := provider.Up(context.Background()); err != nil {
		return fmt.Errorf("goose up: %w", err)
	}
	return nil
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `cd backend && go test ./internal/store/ -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add backend/
git commit -m "feat: add database layer with goose migrations"
```

---

### Task 4: Store — agents CRUD

**Files:**
- Create: `backend/internal/store/agents.go`
- Create: `backend/internal/store/agents_test.go`
- Create: `backend/internal/store/testhelper_test.go`

- [ ] **Step 1: Write test helper**

```go
// backend/internal/store/testhelper_test.go
package store

import "testing"

// newTestDB returns a migrated in-memory SQLite DB for tests.
func newTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
```

- [ ] **Step 2: Write agent store tests**

```go
// backend/internal/store/agents_test.go
package store

import (
	"testing"
	"time"

	"otel-magnify/pkg/models"
)

func TestUpsertAgent(t *testing.T) {
	db := newTestDB(t)

	agent := models.Agent{
		ID:          "agent-001",
		DisplayName: "collector-eu",
		Type:        "collector",
		Version:     "0.96.0",
		Status:      "connected",
		LastSeenAt:  time.Now().UTC().Truncate(time.Second),
		Labels:      models.Labels{"env": "prod"},
	}

	if err := db.UpsertAgent(agent); err != nil {
		t.Fatalf("UpsertAgent: %v", err)
	}

	got, err := db.GetAgent("agent-001")
	if err != nil {
		t.Fatalf("GetAgent: %v", err)
	}
	if got.DisplayName != "collector-eu" {
		t.Errorf("DisplayName = %q, want %q", got.DisplayName, "collector-eu")
	}
	if got.Labels["env"] != "prod" {
		t.Errorf("Labels[env] = %q, want %q", got.Labels["env"], "prod")
	}
}

func TestUpsertAgent_Update(t *testing.T) {
	db := newTestDB(t)

	agent := models.Agent{
		ID: "agent-001", DisplayName: "v1", Type: "collector",
		Status: "connected", LastSeenAt: time.Now().UTC(), Labels: models.Labels{},
	}
	if err := db.UpsertAgent(agent); err != nil {
		t.Fatalf("UpsertAgent: %v", err)
	}

	agent.DisplayName = "v2"
	agent.Status = "degraded"
	if err := db.UpsertAgent(agent); err != nil {
		t.Fatalf("UpsertAgent update: %v", err)
	}

	got, err := db.GetAgent("agent-001")
	if err != nil {
		t.Fatalf("GetAgent: %v", err)
	}
	if got.DisplayName != "v2" || got.Status != "degraded" {
		t.Errorf("got name=%q status=%q, want v2/degraded", got.DisplayName, got.Status)
	}
}

func TestListAgents(t *testing.T) {
	db := newTestDB(t)

	for _, id := range []string{"a1", "a2", "a3"} {
		err := db.UpsertAgent(models.Agent{
			ID: id, Type: "sdk", Status: "connected",
			LastSeenAt: time.Now().UTC(), Labels: models.Labels{},
		})
		if err != nil {
			t.Fatalf("UpsertAgent %s: %v", id, err)
		}
	}

	agents, err := db.ListAgents()
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if len(agents) != 3 {
		t.Errorf("len = %d, want 3", len(agents))
	}
}

func TestUpdateAgentStatus(t *testing.T) {
	db := newTestDB(t)
	err := db.UpsertAgent(models.Agent{
		ID: "a1", Type: "collector", Status: "connected",
		LastSeenAt: time.Now().UTC(), Labels: models.Labels{},
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := db.UpdateAgentStatus("a1", "disconnected"); err != nil {
		t.Fatalf("UpdateAgentStatus: %v", err)
	}

	got, _ := db.GetAgent("a1")
	if got.Status != "disconnected" {
		t.Errorf("Status = %q, want disconnected", got.Status)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd backend && go test ./internal/store/ -run TestUpsertAgent -v`
Expected: FAIL — `UpsertAgent` not defined.

- [ ] **Step 4: Implement agents.go**

```go
// backend/internal/store/agents.go
package store

import (
	"database/sql"
	"fmt"
	"time"

	"otel-magnify/pkg/models"
)

func (d *DB) UpsertAgent(a models.Agent) error {
	labelsJSON, err := a.Labels.Value()
	if err != nil {
		return fmt.Errorf("marshal labels: %w", err)
	}
	_, err = d.Exec(`
		INSERT INTO agents (id, display_name, type, version, status, last_seen_at, labels, active_config_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			display_name = excluded.display_name,
			type = excluded.type,
			version = excluded.version,
			status = excluded.status,
			last_seen_at = excluded.last_seen_at,
			labels = excluded.labels,
			active_config_id = excluded.active_config_id`,
		a.ID, a.DisplayName, a.Type, a.Version, a.Status, a.LastSeenAt.UTC(), labelsJSON, a.ActiveConfigID,
	)
	return err
}

func (d *DB) GetAgent(id string) (models.Agent, error) {
	var a models.Agent
	var labelsJSON string
	err := d.QueryRow(`
		SELECT id, display_name, type, version, status, last_seen_at, labels, active_config_id
		FROM agents WHERE id = ?`, id,
	).Scan(&a.ID, &a.DisplayName, &a.Type, &a.Version, &a.Status, &a.LastSeenAt, &labelsJSON, &a.ActiveConfigID)
	if err != nil {
		return a, fmt.Errorf("get agent %s: %w", id, err)
	}
	if err := a.Labels.Scan(labelsJSON); err != nil {
		return a, fmt.Errorf("scan labels: %w", err)
	}
	return a, nil
}

func (d *DB) ListAgents() ([]models.Agent, error) {
	rows, err := d.Query(`
		SELECT id, display_name, type, version, status, last_seen_at, labels, active_config_id
		FROM agents ORDER BY display_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var agents []models.Agent
	for rows.Next() {
		var a models.Agent
		var labelsJSON string
		if err := rows.Scan(&a.ID, &a.DisplayName, &a.Type, &a.Version, &a.Status, &a.LastSeenAt, &labelsJSON, &a.ActiveConfigID); err != nil {
			return nil, err
		}
		if err := a.Labels.Scan(labelsJSON); err != nil {
			return nil, err
		}
		agents = append(agents, a)
	}
	return agents, rows.Err()
}

func (d *DB) UpdateAgentStatus(id, status string) error {
	res, err := d.Exec(`UPDATE agents SET status = ?, last_seen_at = ? WHERE id = ?`,
		status, time.Now().UTC(), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd backend && go test ./internal/store/ -run TestAgent -v`
Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/store/agents.go backend/internal/store/agents_test.go backend/internal/store/testhelper_test.go
git commit -m "feat: add agent store CRUD operations"
```

---

### Task 5: Store — configs + agent_configs

**Files:**
- Create: `backend/internal/store/configs.go`
- Create: `backend/internal/store/configs_test.go`

- [ ] **Step 1: Write config store tests**

```go
// backend/internal/store/configs_test.go
package store

import (
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	"otel-magnify/pkg/models"
)

func TestCreateConfig(t *testing.T) {
	db := newTestDB(t)

	content := "receivers:\n  otlp:\n    protocols:\n      grpc:"
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(content)))

	cfg := models.Config{
		ID:        hash,
		Name:      "collector-base",
		Content:   content,
		CreatedAt: time.Now().UTC().Truncate(time.Second),
		CreatedBy: "admin@test.com",
	}

	if err := db.CreateConfig(cfg); err != nil {
		t.Fatalf("CreateConfig: %v", err)
	}

	got, err := db.GetConfig(hash)
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if got.Name != "collector-base" {
		t.Errorf("Name = %q, want collector-base", got.Name)
	}
	if got.Content != content {
		t.Errorf("Content mismatch")
	}
}

func TestListConfigs(t *testing.T) {
	db := newTestDB(t)

	for i := range 3 {
		content := fmt.Sprintf("config-%d", i)
		hash := fmt.Sprintf("%x", sha256.Sum256([]byte(content)))
		err := db.CreateConfig(models.Config{
			ID: hash, Name: fmt.Sprintf("cfg-%d", i), Content: content,
			CreatedAt: time.Now().UTC(), CreatedBy: "test",
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	configs, err := db.ListConfigs()
	if err != nil {
		t.Fatal(err)
	}
	if len(configs) != 3 {
		t.Errorf("len = %d, want 3", len(configs))
	}
}

func TestRecordAgentConfig(t *testing.T) {
	db := newTestDB(t)

	// Setup: create config and agent
	err := db.CreateConfig(models.Config{
		ID: "cfg-1", Name: "test", Content: "yaml", CreatedAt: time.Now().UTC(), CreatedBy: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	err = db.UpsertAgent(models.Agent{
		ID: "a1", Type: "collector", Status: "connected",
		LastSeenAt: time.Now().UTC(), Labels: models.Labels{},
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := db.RecordAgentConfig("a1", "cfg-1", "pending"); err != nil {
		t.Fatalf("RecordAgentConfig: %v", err)
	}

	history, err := db.GetAgentConfigHistory("a1")
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 {
		t.Fatalf("len = %d, want 1", len(history))
	}
	if history[0].Status != "pending" {
		t.Errorf("Status = %q, want pending", history[0].Status)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/store/ -run TestCreateConfig -v`
Expected: FAIL — `CreateConfig` not defined.

- [ ] **Step 3: Implement configs.go**

```go
// backend/internal/store/configs.go
package store

import (
	"fmt"
	"time"

	"otel-magnify/pkg/models"
)

func (d *DB) CreateConfig(c models.Config) error {
	_, err := d.Exec(`
		INSERT INTO configs (id, name, content, created_at, created_by)
		VALUES (?, ?, ?, ?, ?)`,
		c.ID, c.Name, c.Content, c.CreatedAt.UTC(), c.CreatedBy,
	)
	return err
}

func (d *DB) GetConfig(id string) (models.Config, error) {
	var c models.Config
	err := d.QueryRow(`SELECT id, name, content, created_at, created_by FROM configs WHERE id = ?`, id).
		Scan(&c.ID, &c.Name, &c.Content, &c.CreatedAt, &c.CreatedBy)
	if err != nil {
		return c, fmt.Errorf("get config %s: %w", id, err)
	}
	return c, nil
}

func (d *DB) ListConfigs() ([]models.Config, error) {
	rows, err := d.Query(`SELECT id, name, content, created_at, created_by FROM configs ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var configs []models.Config
	for rows.Next() {
		var c models.Config
		if err := rows.Scan(&c.ID, &c.Name, &c.Content, &c.CreatedAt, &c.CreatedBy); err != nil {
			return nil, err
		}
		configs = append(configs, c)
	}
	return configs, rows.Err()
}

func (d *DB) RecordAgentConfig(agentID, configID, status string) error {
	_, err := d.Exec(`
		INSERT INTO agent_configs (agent_id, config_id, applied_at, status)
		VALUES (?, ?, ?, ?)`,
		agentID, configID, time.Now().UTC(), status,
	)
	return err
}

func (d *DB) GetAgentConfigHistory(agentID string) ([]models.AgentConfig, error) {
	rows, err := d.Query(`
		SELECT agent_id, config_id, applied_at, status
		FROM agent_configs WHERE agent_id = ? ORDER BY applied_at DESC`, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var history []models.AgentConfig
	for rows.Next() {
		var ac models.AgentConfig
		if err := rows.Scan(&ac.AgentID, &ac.ConfigID, &ac.AppliedAt, &ac.Status); err != nil {
			return nil, err
		}
		history = append(history, ac)
	}
	return history, rows.Err()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && go test ./internal/store/ -run "TestCreateConfig|TestListConfigs|TestRecordAgentConfig" -v`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/store/configs.go backend/internal/store/configs_test.go
git commit -m "feat: add config and agent_config store operations"
```

---

### Task 6: Store — alerts + users

**Files:**
- Create: `backend/internal/store/alerts.go`
- Create: `backend/internal/store/alerts_test.go`
- Create: `backend/internal/store/users.go`
- Create: `backend/internal/store/users_test.go`

- [ ] **Step 1: Write alert store tests**

```go
// backend/internal/store/alerts_test.go
package store

import (
	"testing"
	"time"

	"otel-magnify/pkg/models"
)

func TestCreateAlert(t *testing.T) {
	db := newTestDB(t)
	db.UpsertAgent(models.Agent{
		ID: "a1", Type: "collector", Status: "connected",
		LastSeenAt: time.Now().UTC(), Labels: models.Labels{},
	})

	alert := models.Alert{
		ID:       "alert-001",
		AgentID:  "a1",
		Rule:     "agent_down",
		Severity: "critical",
		Message:  "Agent a1 not seen for 5 minutes",
		FiredAt:  time.Now().UTC().Truncate(time.Second),
	}

	if err := db.CreateAlert(alert); err != nil {
		t.Fatalf("CreateAlert: %v", err)
	}

	alerts, err := db.ListAlerts(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(alerts) != 1 {
		t.Fatalf("len = %d, want 1", len(alerts))
	}
	if alerts[0].Rule != "agent_down" {
		t.Errorf("Rule = %q, want agent_down", alerts[0].Rule)
	}
}

func TestResolveAlert(t *testing.T) {
	db := newTestDB(t)
	db.UpsertAgent(models.Agent{
		ID: "a1", Type: "collector", Status: "connected",
		LastSeenAt: time.Now().UTC(), Labels: models.Labels{},
	})
	db.CreateAlert(models.Alert{
		ID: "alert-001", AgentID: "a1", Rule: "agent_down",
		Severity: "critical", Message: "down", FiredAt: time.Now().UTC(),
	})

	if err := db.ResolveAlert("alert-001"); err != nil {
		t.Fatalf("ResolveAlert: %v", err)
	}

	// Unresolved only
	alerts, err := db.ListAlerts(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(alerts) != 0 {
		t.Errorf("unresolved count = %d, want 0", len(alerts))
	}

	// Including resolved
	all, err := db.ListAlerts(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].ResolvedAt == nil {
		t.Error("expected 1 resolved alert")
	}
}

func TestGetUnresolvedAlertByAgentAndRule(t *testing.T) {
	db := newTestDB(t)
	db.UpsertAgent(models.Agent{
		ID: "a1", Type: "collector", Status: "connected",
		LastSeenAt: time.Now().UTC(), Labels: models.Labels{},
	})
	db.CreateAlert(models.Alert{
		ID: "alert-001", AgentID: "a1", Rule: "agent_down",
		Severity: "critical", Message: "down", FiredAt: time.Now().UTC(),
	})

	alert, err := db.GetUnresolvedAlertByAgentAndRule("a1", "agent_down")
	if err != nil {
		t.Fatalf("GetUnresolvedAlertByAgentAndRule: %v", err)
	}
	if alert == nil {
		t.Fatal("expected alert, got nil")
	}
	if alert.ID != "alert-001" {
		t.Errorf("ID = %q, want alert-001", alert.ID)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/store/ -run TestCreateAlert -v`
Expected: FAIL — `CreateAlert` not defined.

- [ ] **Step 3: Implement alerts.go**

```go
// backend/internal/store/alerts.go
package store

import (
	"database/sql"
	"time"

	"otel-magnify/pkg/models"
)

func (d *DB) CreateAlert(a models.Alert) error {
	_, err := d.Exec(`
		INSERT INTO alerts (id, agent_id, rule, severity, message, fired_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		a.ID, a.AgentID, a.Rule, a.Severity, a.Message, a.FiredAt.UTC(),
	)
	return err
}

func (d *DB) ResolveAlert(id string) error {
	_, err := d.Exec(`UPDATE alerts SET resolved_at = ? WHERE id = ?`, time.Now().UTC(), id)
	return err
}

func (d *DB) ListAlerts(includeResolved bool) ([]models.Alert, error) {
	query := `SELECT id, agent_id, rule, severity, message, fired_at, resolved_at FROM alerts`
	if !includeResolved {
		query += ` WHERE resolved_at IS NULL`
	}
	query += ` ORDER BY fired_at DESC`

	rows, err := d.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var alerts []models.Alert
	for rows.Next() {
		var a models.Alert
		if err := rows.Scan(&a.ID, &a.AgentID, &a.Rule, &a.Severity, &a.Message, &a.FiredAt, &a.ResolvedAt); err != nil {
			return nil, err
		}
		alerts = append(alerts, a)
	}
	return alerts, rows.Err()
}

func (d *DB) GetUnresolvedAlertByAgentAndRule(agentID, rule string) (*models.Alert, error) {
	var a models.Alert
	err := d.QueryRow(`
		SELECT id, agent_id, rule, severity, message, fired_at, resolved_at
		FROM alerts WHERE agent_id = ? AND rule = ? AND resolved_at IS NULL
		LIMIT 1`, agentID, rule,
	).Scan(&a.ID, &a.AgentID, &a.Rule, &a.Severity, &a.Message, &a.FiredAt, &a.ResolvedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}
```

- [ ] **Step 4: Write user store tests**

```go
// backend/internal/store/users_test.go
package store

import (
	"testing"

	"otel-magnify/pkg/models"

	"golang.org/x/crypto/bcrypt"
)

func TestCreateUser(t *testing.T) {
	db := newTestDB(t)

	hash, _ := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.DefaultCost)
	user := models.User{
		ID:           "user-001",
		Email:        "admin@test.com",
		PasswordHash: string(hash),
		Role:         "admin",
	}

	if err := db.CreateUser(user); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	got, err := db.GetUserByEmail("admin@test.com")
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	if got.Role != "admin" {
		t.Errorf("Role = %q, want admin", got.Role)
	}
	if bcrypt.CompareHashAndPassword([]byte(got.PasswordHash), []byte("secret")) != nil {
		t.Error("password hash mismatch")
	}
}

func TestGetUserByEmail_NotFound(t *testing.T) {
	db := newTestDB(t)

	_, err := db.GetUserByEmail("nobody@test.com")
	if err == nil {
		t.Error("expected error for non-existent user")
	}
}
```

- [ ] **Step 5: Implement users.go**

```go
// backend/internal/store/users.go
package store

import (
	"fmt"

	"otel-magnify/pkg/models"
)

func (d *DB) CreateUser(u models.User) error {
	_, err := d.Exec(`
		INSERT INTO users (id, email, password_hash, role, tenant_id)
		VALUES (?, ?, ?, ?, ?)`,
		u.ID, u.Email, u.PasswordHash, u.Role, u.TenantID,
	)
	return err
}

func (d *DB) GetUserByEmail(email string) (models.User, error) {
	var u models.User
	err := d.QueryRow(`
		SELECT id, email, password_hash, role, tenant_id
		FROM users WHERE email = ?`, email,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.TenantID)
	if err != nil {
		return u, fmt.Errorf("get user by email %s: %w", email, err)
	}
	return u, nil
}
```

- [ ] **Step 6: Add bcrypt dependency and run all store tests**

```bash
cd backend && go get golang.org/x/crypto/bcrypt
```

Run: `cd backend && go test ./internal/store/ -v`
Expected: all PASS.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/store/alerts.go backend/internal/store/alerts_test.go \
       backend/internal/store/users.go backend/internal/store/users_test.go \
       backend/go.mod backend/go.sum
git commit -m "feat: add alert and user store operations"
```

---

## Phase 2: Backend Core

### Task 7: OpAMP server integration

**Files:**
- Create: `backend/internal/opamp/server.go`
- Create: `backend/internal/opamp/server_test.go`

- [ ] **Step 1: Add opamp-go dependency**

```bash
cd backend && go get github.com/open-telemetry/opamp-go
```

- [ ] **Step 2: Write failing test**

```go
// backend/internal/opamp/server_test.go
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
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd backend && go test ./internal/opamp/ -run TestNewOpAMPServer -v`
Expected: FAIL — `New` not defined.

- [ ] **Step 4: Implement opamp/server.go**

```go
// backend/internal/opamp/server.go
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

	opampServer "github.com/open-telemetry/opamp-go/server"
	"github.com/open-telemetry/opamp-go/server/types"
	"github.com/open-telemetry/opamp-go/protobufs"

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
	conns map[string]types.Connection // agentUID hex → connection
}

func New(db *store.DB, notifier Notifier) *Server {
	return &Server{
		opamp:    opampServer.New(nil),
		store:    db,
		notifier: notifier,
		conns:    make(map[string]types.Connection),
	}
}

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
		// Non-identifying attributes → labels
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
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd backend && go test ./internal/opamp/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/opamp/ backend/go.mod backend/go.sum
git commit -m "feat: add OpAMP server with agent registration and config push"
```

---

### Task 8: JWT authentication

**Files:**
- Create: `backend/internal/auth/auth.go`
- Create: `backend/internal/auth/auth_test.go`

- [ ] **Step 1: Add JWT dependency**

```bash
cd backend && go get github.com/golang-jwt/jwt/v5
```

- [ ] **Step 2: Write failing tests**

```go
// backend/internal/auth/auth_test.go
package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGenerateAndValidateToken(t *testing.T) {
	a := New("test-secret-key-at-least-32-bytes!")

	token, err := a.GenerateToken("user-001", "admin@test.com", "admin")
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	claims, err := a.ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
	if claims.UserID != "user-001" {
		t.Errorf("UserID = %q, want user-001", claims.UserID)
	}
	if claims.Email != "admin@test.com" {
		t.Errorf("Email = %q, want admin@test.com", claims.Email)
	}
	if claims.Role != "admin" {
		t.Errorf("Role = %q, want admin", claims.Role)
	}
}

func TestValidateToken_Invalid(t *testing.T) {
	a := New("test-secret-key-at-least-32-bytes!")
	_, err := a.ValidateToken("garbage.token.here")
	if err == nil {
		t.Error("expected error for invalid token")
	}
}

func TestMiddleware_NoToken(t *testing.T) {
	a := New("test-secret-key-at-least-32-bytes!")

	handler := a.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("GET", "/api/agents", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestMiddleware_ValidToken(t *testing.T) {
	a := New("test-secret-key-at-least-32-bytes!")

	token, _ := a.GenerateToken("user-001", "admin@test.com", "admin")

	handler := a.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims := ClaimsFromContext(r.Context())
		if claims == nil {
			t.Error("expected claims in context")
		}
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("GET", "/api/agents", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd backend && go test ./internal/auth/ -run TestGenerateAndValidateToken -v`
Expected: FAIL — `New` not defined.

- [ ] **Step 4: Implement auth.go**

```go
// backend/internal/auth/auth.go
package auth

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type contextKey struct{}

type Claims struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
	Role   string `json:"role"`
	jwt.RegisteredClaims
}

type Auth struct {
	secret []byte
}

func New(secret string) *Auth {
	return &Auth{secret: []byte(secret)}
}

func (a *Auth) GenerateToken(userID, email, role string) (string, error) {
	claims := Claims{
		UserID: userID,
		Email:  email,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(a.secret)
}

func (a *Auth) ValidateToken(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return a.secret, nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}
	return claims, nil
}

func (a *Auth) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if header == "" || !strings.HasPrefix(header, "Bearer ") {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		tokenStr := strings.TrimPrefix(header, "Bearer ")
		claims, err := a.ValidateToken(tokenStr)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), contextKey{}, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func ClaimsFromContext(ctx context.Context) *Claims {
	claims, _ := ctx.Value(contextKey{}).(*Claims)
	return claims
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd backend && go test ./internal/auth/ -v`
Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/auth/ backend/go.mod backend/go.sum
git commit -m "feat: add JWT auth with middleware"
```

---

### Task 9: WebSocket hub for frontend

**Files:**
- Create: `backend/internal/api/wshub.go`
- Create: `backend/internal/api/wshub_test.go`

- [ ] **Step 1: Add gorilla/websocket dependency**

```bash
cd backend && go get github.com/gorilla/websocket
```

Note: `gorilla/websocket` is already an indirect dependency via `opamp-go`, but we add it as a direct dependency here.

- [ ] **Step 2: Write failing test**

```go
// backend/internal/api/wshub_test.go
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
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd backend && go test ./internal/api/ -run TestHub -v`
Expected: FAIL — `NewHub` not defined.

- [ ] **Step 4: Implement wshub.go**

```go
// backend/internal/api/wshub.go
package api

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"

	"otel-magnify/pkg/models"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type wsClient struct {
	conn *websocket.Conn
	send chan []byte
}

type Hub struct {
	clients    map[*wsClient]bool
	broadcast  chan []byte
	register   chan *wsClient
	unregister chan *wsClient
	mu         sync.Mutex
	done       chan struct{}
}

func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*wsClient]bool),
		broadcast:  make(chan []byte, 256),
		register:   make(chan *wsClient),
		unregister: make(chan *wsClient),
		done:       make(chan struct{}),
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()
		case msg := <-h.broadcast:
			h.mu.Lock()
			for client := range h.clients {
				select {
				case client.send <- msg:
				default:
					delete(h.clients, client)
					close(client.send)
				}
			}
			h.mu.Unlock()
		case <-h.done:
			return
		}
	}
}

func (h *Hub) Stop() {
	close(h.done)
}

func (h *Hub) BroadcastAgentUpdate(agent models.Agent) {
	event := map[string]any{
		"type":  "agent_update",
		"agent": agent,
	}
	data, err := json.Marshal(event)
	if err != nil {
		log.Printf("marshal agent update: %v", err)
		return
	}
	h.broadcast <- data
}

func (h *Hub) BroadcastAlertUpdate(alert models.Alert) {
	event := map[string]any{
		"type":  "alert_update",
		"alert": alert,
	}
	data, err := json.Marshal(event)
	if err != nil {
		log.Printf("marshal alert update: %v", err)
		return
	}
	h.broadcast <- data
}

func (h *Hub) HandleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade: %v", err)
		return
	}

	client := &wsClient{
		conn: conn,
		send: make(chan []byte, 256),
	}
	h.register <- client

	go client.writePump()
	go client.readPump(h)
}

func (c *wsClient) writePump() {
	defer c.conn.Close()
	for msg := range c.send {
		if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			return
		}
	}
}

func (c *wsClient) readPump(h *Hub) {
	defer func() {
		h.unregister <- c
		c.conn.Close()
	}()
	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			break
		}
	}
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd backend && go test ./internal/api/ -run TestHub -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/api/wshub.go backend/internal/api/wshub_test.go backend/go.mod backend/go.sum
git commit -m "feat: add WebSocket hub for real-time frontend updates"
```

---

### Task 10: REST API — router + agent handlers

**Files:**
- Create: `backend/internal/api/router.go`
- Create: `backend/internal/api/agents.go`
- Create: `backend/internal/api/agents_test.go`

- [ ] **Step 1: Add chi dependency**

```bash
cd backend && go get github.com/go-chi/chi/v5
```

- [ ] **Step 2: Write failing test**

```go
// backend/internal/api/agents_test.go
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"otel-magnify/internal/auth"
	"otel-magnify/internal/store"
	"otel-magnify/pkg/models"
)

func newTestAPI(t *testing.T) (*store.DB, http.Handler) {
	t.Helper()
	db, err := store.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	a := auth.New("test-secret-key-at-least-32-bytes!")
	hub := NewHub()
	go hub.Run()
	t.Cleanup(hub.Stop)

	router := NewRouter(db, a, hub, nil)
	return db, router
}

func authedRequest(t *testing.T, method, url string) *http.Request {
	t.Helper()
	a := auth.New("test-secret-key-at-least-32-bytes!")
	token, _ := a.GenerateToken("user-001", "admin@test.com", "admin")
	req := httptest.NewRequest(method, url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}

func TestListAgents_Empty(t *testing.T) {
	_, router := newTestAPI(t)
	req := authedRequest(t, "GET", "/api/agents")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var agents []models.Agent
	json.NewDecoder(rec.Body).Decode(&agents)
	if agents != nil {
		t.Errorf("expected nil or empty slice, got %v", agents)
	}
}

func TestListAgents_WithData(t *testing.T) {
	db, router := newTestAPI(t)

	db.UpsertAgent(models.Agent{
		ID: "a1", DisplayName: "test", Type: "collector",
		Status: "connected", LastSeenAt: time.Now().UTC(), Labels: models.Labels{},
	})

	req := authedRequest(t, "GET", "/api/agents")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var agents []models.Agent
	json.NewDecoder(rec.Body).Decode(&agents)
	if len(agents) != 1 {
		t.Errorf("len = %d, want 1", len(agents))
	}
}

func TestGetAgent(t *testing.T) {
	db, router := newTestAPI(t)
	db.UpsertAgent(models.Agent{
		ID: "a1", DisplayName: "test", Type: "collector",
		Status: "connected", LastSeenAt: time.Now().UTC(), Labels: models.Labels{},
	})

	req := authedRequest(t, "GET", "/api/agents/a1")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var agent models.Agent
	json.NewDecoder(rec.Body).Decode(&agent)
	if agent.ID != "a1" {
		t.Errorf("ID = %q, want a1", agent.ID)
	}
}

func TestGetAgent_NotFound(t *testing.T) {
	_, router := newTestAPI(t)

	req := authedRequest(t, "GET", "/api/agents/nonexistent")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != 404 {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd backend && go test ./internal/api/ -run TestListAgents -v`
Expected: FAIL — `NewRouter` not defined.

- [ ] **Step 4: Implement router.go**

```go
// backend/internal/api/router.go
package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"otel-magnify/internal/auth"
	"otel-magnify/internal/opamp"
	"otel-magnify/internal/store"
)

type API struct {
	db    *store.DB
	auth  *auth.Auth
	hub   *Hub
	opamp *opamp.Server
}

func NewRouter(db *store.DB, a *auth.Auth, hub *Hub, opampSrv *opamp.Server) http.Handler {
	api := &API{db: db, auth: a, hub: hub, opamp: opampSrv}

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// Public routes
	r.Post("/api/auth/login", api.handleLogin)

	// Protected routes
	r.Group(func(r chi.Router) {
		r.Use(a.Middleware)

		r.Get("/api/agents", api.handleListAgents)
		r.Get("/api/agents/{id}", api.handleGetAgent)
		r.Post("/api/agents/{id}/config", api.handlePushConfig)

		r.Get("/api/configs", api.handleListConfigs)
		r.Post("/api/configs", api.handleCreateConfig)
		r.Get("/api/configs/{id}", api.handleGetConfig)

		r.Get("/api/alerts", api.handleListAlerts)
		r.Post("/api/alerts/{id}/resolve", api.handleResolveAlert)
	})

	// WebSocket for frontend (authenticated via query param)
	r.Get("/ws", hub.HandleWS)

	return r
}

func respondJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func respondError(w http.ResponseWriter, status int, msg string) {
	respondJSON(w, status, map[string]string{"error": msg})
}
```

- [ ] **Step 5: Implement agents.go handlers**

```go
// backend/internal/api/agents.go
package api

import (
	"database/sql"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
)

func (a *API) handleListAgents(w http.ResponseWriter, r *http.Request) {
	agents, err := a.db.ListAgents()
	if err != nil {
		respondError(w, 500, "failed to list agents")
		return
	}
	respondJSON(w, 200, agents)
}

func (a *API) handleGetAgent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	agent, err := a.db.GetAgent(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			respondError(w, 404, "agent not found")
			return
		}
		respondError(w, 500, "failed to get agent")
		return
	}
	respondJSON(w, 200, agent)
}

func (a *API) handlePushConfig(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "id")

	body, err := io.ReadAll(r.Body)
	if err != nil {
		respondError(w, 400, "failed to read body")
		return
	}
	defer r.Body.Close()

	if a.opamp == nil {
		respondError(w, 503, "OpAMP server not available")
		return
	}

	if err := a.opamp.PushConfig(r.Context(), agentID, body); err != nil {
		respondError(w, 502, err.Error())
		return
	}

	respondJSON(w, 202, map[string]string{"status": "config push initiated"})
}
```

- [ ] **Step 6: Add stub handlers for routes that aren't implemented yet**

```go
// backend/internal/api/configs.go
package api

import "net/http"

func (a *API) handleListConfigs(w http.ResponseWriter, r *http.Request)  { respondJSON(w, 200, nil) }
func (a *API) handleCreateConfig(w http.ResponseWriter, r *http.Request) { respondJSON(w, 501, nil) }
func (a *API) handleGetConfig(w http.ResponseWriter, r *http.Request)    { respondJSON(w, 501, nil) }
```

```go
// backend/internal/api/alerts.go
package api

import "net/http"

func (a *API) handleListAlerts(w http.ResponseWriter, r *http.Request)  { respondJSON(w, 200, nil) }
func (a *API) handleResolveAlert(w http.ResponseWriter, r *http.Request) { respondJSON(w, 501, nil) }
```

```go
// backend/internal/api/authhandler.go
package api

import "net/http"

func (a *API) handleLogin(w http.ResponseWriter, r *http.Request) { respondJSON(w, 501, nil) }
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `cd backend && go test ./internal/api/ -v`
Expected: all PASS.

- [ ] **Step 8: Commit**

```bash
git add backend/internal/api/ backend/go.mod backend/go.sum
git commit -m "feat: add REST API router with agent handlers"
```

---

### Task 11: REST API — configs + alerts + auth handlers

**Files:**
- Modify: `backend/internal/api/configs.go`
- Modify: `backend/internal/api/alerts.go`
- Modify: `backend/internal/api/authhandler.go`
- Create: `backend/internal/api/configs_test.go`

- [ ] **Step 1: Write configs handler tests**

```go
// backend/internal/api/configs_test.go
package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"otel-magnify/internal/auth"
	"otel-magnify/pkg/models"
)

func TestCreateAndListConfigs(t *testing.T) {
	_, router := newTestAPI(t)

	body := `{"name":"collector-base","content":"receivers:\n  otlp:"}`
	a := auth.New("test-secret-key-at-least-32-bytes!")
	token, _ := a.GenerateToken("user-001", "admin@test.com", "admin")

	req := httptest.NewRequest("POST", "/api/configs", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != 201 {
		t.Fatalf("create status = %d, body = %s", rec.Code, rec.Body.String())
	}

	req = authedRequest(t, "GET", "/api/configs")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("list status = %d", rec.Code)
	}

	var configs []models.Config
	json.NewDecoder(rec.Body).Decode(&configs)
	if len(configs) != 1 {
		t.Errorf("len = %d, want 1", len(configs))
	}
}

func TestLoginHandler(t *testing.T) {
	db, router := newTestAPI(t)

	// Create a user with known password
	hash, _ := hashPassword("testpass123")
	db.CreateUser(models.User{
		ID: "user-001", Email: "admin@test.com", PasswordHash: hash, Role: "admin",
	})

	body := `{"email":"admin@test.com","password":"testpass123"}`
	req := httptest.NewRequest("POST", "/api/auth/login", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("login status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var resp map[string]string
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["token"] == "" {
		t.Error("expected non-empty token")
	}
}

func TestListAlerts_Handler(t *testing.T) {
	db, router := newTestAPI(t)
	db.UpsertAgent(models.Agent{
		ID: "a1", Type: "collector", Status: "connected",
		LastSeenAt: time.Now().UTC(), Labels: models.Labels{},
	})
	db.CreateAlert(models.Alert{
		ID: "alert-1", AgentID: "a1", Rule: "agent_down",
		Severity: "critical", Message: "down", FiredAt: time.Now().UTC(),
	})

	req := authedRequest(t, "GET", "/api/alerts")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}

	var alerts []models.Alert
	json.NewDecoder(rec.Body).Decode(&alerts)
	if len(alerts) != 1 {
		t.Errorf("len = %d, want 1", len(alerts))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/api/ -run TestCreateAndListConfigs -v`
Expected: FAIL — `handleCreateConfig` returns 501.

- [ ] **Step 3: Implement full configs.go**

```go
// backend/internal/api/configs.go
package api

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"otel-magnify/internal/auth"
	"otel-magnify/pkg/models"
)

type createConfigRequest struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

func (a *API) handleListConfigs(w http.ResponseWriter, r *http.Request) {
	configs, err := a.db.ListConfigs()
	if err != nil {
		respondError(w, 500, "failed to list configs")
		return
	}
	respondJSON(w, 200, configs)
}

func (a *API) handleCreateConfig(w http.ResponseWriter, r *http.Request) {
	var req createConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, 400, "invalid JSON")
		return
	}
	if req.Name == "" || req.Content == "" {
		respondError(w, 400, "name and content are required")
		return
	}

	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(req.Content)))
	claims := auth.ClaimsFromContext(r.Context())
	createdBy := ""
	if claims != nil {
		createdBy = claims.Email
	}

	cfg := models.Config{
		ID:        hash,
		Name:      req.Name,
		Content:   req.Content,
		CreatedAt: time.Now().UTC(),
		CreatedBy: createdBy,
	}

	if err := a.db.CreateConfig(cfg); err != nil {
		respondError(w, 500, "failed to create config")
		return
	}
	respondJSON(w, 201, cfg)
}

func (a *API) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	cfg, err := a.db.GetConfig(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			respondError(w, 404, "config not found")
			return
		}
		respondError(w, 500, "failed to get config")
		return
	}
	respondJSON(w, 200, cfg)
}
```

- [ ] **Step 4: Implement full alerts.go**

```go
// backend/internal/api/alerts.go
package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func (a *API) handleListAlerts(w http.ResponseWriter, r *http.Request) {
	includeResolved := r.URL.Query().Get("include_resolved") == "true"
	alerts, err := a.db.ListAlerts(includeResolved)
	if err != nil {
		respondError(w, 500, "failed to list alerts")
		return
	}
	respondJSON(w, 200, alerts)
}

func (a *API) handleResolveAlert(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := a.db.ResolveAlert(id); err != nil {
		respondError(w, 500, "failed to resolve alert")
		return
	}
	respondJSON(w, 200, map[string]string{"status": "resolved"})
}
```

- [ ] **Step 5: Implement full authhandler.go**

```go
// backend/internal/api/authhandler.go
package api

import (
	"encoding/json"
	"net/http"

	"golang.org/x/crypto/bcrypt"
)

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func hashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	return string(hash), err
}

func (a *API) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, 400, "invalid JSON")
		return
	}
	if req.Email == "" || req.Password == "" {
		respondError(w, 400, "email and password are required")
		return
	}

	user, err := a.db.GetUserByEmail(req.Email)
	if err != nil {
		respondError(w, 401, "invalid credentials")
		return
	}

	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)) != nil {
		respondError(w, 401, "invalid credentials")
		return
	}

	token, err := a.auth.GenerateToken(user.ID, user.Email, user.Role)
	if err != nil {
		respondError(w, 500, "failed to generate token")
		return
	}

	respondJSON(w, 200, map[string]string{"token": token})
}
```

- [ ] **Step 6: Run all API tests**

Run: `cd backend && go test ./internal/api/ -v`
Expected: all PASS.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/api/
git commit -m "feat: implement config, alert, and auth REST handlers"
```

---

### Task 12: Alert engine

**Files:**
- Create: `backend/internal/alerts/engine.go`
- Create: `backend/internal/alerts/engine_test.go`

- [ ] **Step 1: Write failing test**

```go
// backend/internal/alerts/engine_test.go
package alerts

import (
	"testing"
	"time"

	"otel-magnify/internal/store"
	"otel-magnify/pkg/models"
)

func newTestDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestEvaluate_AgentDown(t *testing.T) {
	db := newTestDB(t)

	// Agent last seen 10 minutes ago
	db.UpsertAgent(models.Agent{
		ID: "a1", Type: "collector", Status: "connected",
		LastSeenAt: time.Now().UTC().Add(-10 * time.Minute), Labels: models.Labels{},
	})

	engine := New(db, nil, 5*time.Minute)
	engine.Evaluate()

	alerts, err := db.ListAlerts(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(alerts) != 1 {
		t.Fatalf("len = %d, want 1", len(alerts))
	}
	if alerts[0].Rule != "agent_down" {
		t.Errorf("Rule = %q, want agent_down", alerts[0].Rule)
	}
}

func TestEvaluate_AgentDown_NoDouble(t *testing.T) {
	db := newTestDB(t)

	db.UpsertAgent(models.Agent{
		ID: "a1", Type: "collector", Status: "connected",
		LastSeenAt: time.Now().UTC().Add(-10 * time.Minute), Labels: models.Labels{},
	})

	engine := New(db, nil, 5*time.Minute)
	engine.Evaluate()
	engine.Evaluate()

	alerts, err := db.ListAlerts(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(alerts) != 1 {
		t.Errorf("len = %d, want 1 (no duplicates)", len(alerts))
	}
}

func TestEvaluate_AgentRecovers(t *testing.T) {
	db := newTestDB(t)

	// Agent was down
	db.UpsertAgent(models.Agent{
		ID: "a1", Type: "collector", Status: "connected",
		LastSeenAt: time.Now().UTC().Add(-10 * time.Minute), Labels: models.Labels{},
	})
	engine := New(db, nil, 5*time.Minute)
	engine.Evaluate()

	// Agent comes back
	db.UpsertAgent(models.Agent{
		ID: "a1", Type: "collector", Status: "connected",
		LastSeenAt: time.Now().UTC(), Labels: models.Labels{},
	})
	engine.Evaluate()

	alerts, err := db.ListAlerts(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(alerts) != 0 {
		t.Errorf("unresolved alerts = %d, want 0", len(alerts))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/alerts/ -run TestEvaluate -v`
Expected: FAIL — `New` not defined.

- [ ] **Step 3: Implement engine.go**

```go
// backend/internal/alerts/engine.go
package alerts

import (
	"context"
	"log"
	"time"

	"otel-magnify/internal/api"
	"otel-magnify/internal/store"
	"otel-magnify/pkg/models"

	"crypto/rand"
	"encoding/hex"
)

type Engine struct {
	db           *store.DB
	hub          *api.Hub
	downTimeout  time.Duration
}

func New(db *store.DB, hub *api.Hub, downTimeout time.Duration) *Engine {
	return &Engine{
		db:          db,
		hub:         hub,
		downTimeout: downTimeout,
	}
}

func (e *Engine) Start(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			e.Evaluate()
		case <-ctx.Done():
			return
		}
	}
}

func (e *Engine) Evaluate() {
	agents, err := e.db.ListAgents()
	if err != nil {
		log.Printf("alert engine: list agents: %v", err)
		return
	}

	now := time.Now().UTC()
	for _, agent := range agents {
		e.evaluateAgentDown(agent, now)
	}
}

func (e *Engine) evaluateAgentDown(agent models.Agent, now time.Time) {
	isDown := now.Sub(agent.LastSeenAt) > e.downTimeout

	existing, err := e.db.GetUnresolvedAlertByAgentAndRule(agent.ID, "agent_down")
	if err != nil {
		log.Printf("alert engine: check existing alert for %s: %v", agent.ID, err)
		return
	}

	if isDown && existing == nil {
		alert := models.Alert{
			ID:       generateID(),
			AgentID:  agent.ID,
			Rule:     "agent_down",
			Severity: "critical",
			Message:  "Agent " + agent.ID + " not seen for " + e.downTimeout.String(),
			FiredAt:  now,
		}
		if err := e.db.CreateAlert(alert); err != nil {
			log.Printf("alert engine: create alert: %v", err)
			return
		}
		if e.hub != nil {
			e.hub.BroadcastAlertUpdate(alert)
		}
	}

	if !isDown && existing != nil {
		if err := e.db.ResolveAlert(existing.ID); err != nil {
			log.Printf("alert engine: resolve alert: %v", err)
		}
	}
}

func generateID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && go test ./internal/alerts/ -v`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/alerts/
git commit -m "feat: add alert engine with agent_down rule evaluation"
```

---

### Task 13: Server entrypoint

**Files:**
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 1: Wire everything together**

```go
// backend/cmd/server/main.go
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"otel-magnify/internal/alerts"
	"otel-magnify/internal/api"
	"otel-magnify/internal/auth"
	"otel-magnify/internal/config"
	"otel-magnify/internal/opamp"
	"otel-magnify/internal/store"
)

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

	// WebSocket hub
	hub := api.NewHub()
	go hub.Run()

	// OpAMP server
	opampSrv := opamp.New(db, hub)
	opampHandler, connCtx, err := opampSrv.Attach()
	if err != nil {
		log.Fatalf("Failed to attach OpAMP server: %v", err)
	}

	// Start OpAMP HTTP server on separate port
	opampMux := http.NewServeMux()
	opampMux.HandleFunc("/v1/opamp", opampHandler)
	opampHTTP := &http.Server{
		Addr:        cfg.OpAMPAddr,
		Handler:     opampMux,
		ConnContext: connCtx,
	}
	go func() {
		log.Printf("OpAMP server listening on %s", cfg.OpAMPAddr)
		if err := opampHTTP.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("OpAMP server: %v", err)
		}
	}()

	// Alert engine
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	alertEngine := alerts.New(db, hub, 5*time.Minute)
	go alertEngine.Start(ctx, 30*time.Second)
	log.Println("Alert engine started (30s interval)")

	// REST API
	a := auth.New(cfg.JWTSecret)
	router := api.NewRouter(db, a, hub, opampSrv)

	apiHTTP := &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: router,
	}

	go func() {
		log.Printf("API server listening on %s", cfg.ListenAddr)
		if err := apiHTTP.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("API server: %v", err)
		}
	}()

	// Graceful shutdown
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	log.Println("Shutting down...")
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	apiHTTP.Shutdown(shutdownCtx)
	opampHTTP.Shutdown(shutdownCtx)
	opampSrv.Stop(shutdownCtx)
	hub.Stop()
	log.Println("Shutdown complete")
}
```

- [ ] **Step 2: Verify compilation**

Run: `cd backend && go build ./cmd/server/`
Expected: binary builds with no errors.

- [ ] **Step 3: Run all backend tests**

Run: `cd backend && go test ./... -v`
Expected: all PASS.

- [ ] **Step 4: Commit**

```bash
git add backend/cmd/server/main.go
git commit -m "feat: wire server entrypoint with all backend components"
```

---

## Phase 3: Frontend

### Task 14: Frontend scaffold

**Files:**
- Create: `frontend/` (Vite React TypeScript project)

- [ ] **Step 1: Create Vite project**

```bash
cd /home/dev/projets/otel-magnify
npm create vite@latest frontend -- --template react-ts
```

- [ ] **Step 2: Install dependencies**

```bash
cd frontend
npm install
npm install react-router-dom@6 zustand @tanstack/react-query axios recharts
npm install @codemirror/lang-yaml @codemirror/view @codemirror/state codemirror @codemirror/basic-setup
npm install -D @types/react-router-dom
```

- [ ] **Step 3: Configure Vite proxy for API**

```typescript
// frontend/vite.config.ts
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/api': 'http://localhost:8080',
      '/ws': {
        target: 'ws://localhost:8080',
        ws: true,
      },
    },
  },
})
```

- [ ] **Step 4: Verify it runs**

Run: `cd frontend && npm run dev -- --host 0.0.0.0 &` then `curl -s http://localhost:5173 | head -5`
Expected: HTML response with `<div id="root">`.

Kill the dev server after verifying.

- [ ] **Step 5: Commit**

```bash
git add frontend/
git commit -m "feat: scaffold React frontend with Vite"
```

---

### Task 15: Frontend API client + WebSocket + Zustand store

**Files:**
- Create: `frontend/src/api/client.ts`
- Create: `frontend/src/api/websocket.ts`
- Create: `frontend/src/store/index.ts`
- Create: `frontend/src/types.ts`

- [ ] **Step 1: Write shared types**

```typescript
// frontend/src/types.ts
export interface Agent {
  id: string
  display_name: string
  type: 'collector' | 'sdk'
  version: string
  status: 'connected' | 'disconnected' | 'degraded'
  last_seen_at: string
  labels: Record<string, string>
  active_config_id?: string
}

export interface Config {
  id: string
  name: string
  content: string
  created_at: string
  created_by: string
}

export interface Alert {
  id: string
  agent_id: string
  rule: 'agent_down' | 'config_drift' | 'version_outdated'
  severity: 'warning' | 'critical'
  message: string
  fired_at: string
  resolved_at?: string
}

export interface AgentConfig {
  agent_id: string
  config_id: string
  applied_at: string
  status: 'pending' | 'applied' | 'failed'
}
```

- [ ] **Step 2: Write API client**

```typescript
// frontend/src/api/client.ts
import axios from 'axios'
import type { Agent, Config, Alert, AgentConfig } from '../types'

const api = axios.create({ baseURL: '/api' })

api.interceptors.request.use((config) => {
  const token = localStorage.getItem('token')
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

api.interceptors.response.use(
  (res) => res,
  (err) => {
    if (err.response?.status === 401) {
      localStorage.removeItem('token')
      window.location.href = '/login'
    }
    return Promise.reject(err)
  }
)

export const agentsAPI = {
  list: () => api.get<Agent[]>('/agents').then((r) => r.data ?? []),
  get: (id: string) => api.get<Agent>(`/agents/${id}`).then((r) => r.data),
  pushConfig: (id: string, yaml: string) =>
    api.post(`/agents/${id}/config`, yaml, { headers: { 'Content-Type': 'text/yaml' } }),
  getConfigHistory: (id: string) =>
    api.get<AgentConfig[]>(`/agents/${id}/configs`).then((r) => r.data ?? []),
}

export const configsAPI = {
  list: () => api.get<Config[]>('/configs').then((r) => r.data ?? []),
  get: (id: string) => api.get<Config>(`/configs/${id}`).then((r) => r.data),
  create: (name: string, content: string) =>
    api.post<Config>('/configs', { name, content }).then((r) => r.data),
}

export const alertsAPI = {
  list: (includeResolved = false) =>
    api.get<Alert[]>('/alerts', { params: { include_resolved: includeResolved } }).then((r) => r.data ?? []),
  resolve: (id: string) => api.post(`/alerts/${id}/resolve`),
}

export const authAPI = {
  login: (email: string, password: string) =>
    api.post<{ token: string }>('/auth/login', { email, password }).then((r) => r.data),
}

export default api
```

- [ ] **Step 3: Write WebSocket client**

```typescript
// frontend/src/api/websocket.ts
import { useStore } from '../store'

let ws: WebSocket | null = null
let reconnectTimer: ReturnType<typeof setTimeout> | null = null

export function connectWS() {
  if (ws?.readyState === WebSocket.OPEN) return

  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  ws = new WebSocket(`${protocol}//${window.location.host}/ws`)

  ws.onmessage = (event) => {
    const data = JSON.parse(event.data)
    const store = useStore.getState()

    switch (data.type) {
      case 'agent_update':
        store.updateAgent(data.agent)
        break
      case 'alert_update':
        store.addAlert(data.alert)
        break
    }
  }

  ws.onclose = () => {
    reconnectTimer = setTimeout(connectWS, 3000)
  }

  ws.onerror = () => {
    ws?.close()
  }
}

export function disconnectWS() {
  if (reconnectTimer) clearTimeout(reconnectTimer)
  ws?.close()
  ws = null
}
```

- [ ] **Step 4: Write Zustand store**

```typescript
// frontend/src/store/index.ts
import { create } from 'zustand'
import type { Agent, Alert } from '../types'

interface AppState {
  agents: Agent[]
  alerts: Alert[]
  setAgents: (agents: Agent[]) => void
  updateAgent: (agent: Agent) => void
  setAlerts: (alerts: Alert[]) => void
  addAlert: (alert: Alert) => void
  resolveAlert: (id: string) => void
}

export const useStore = create<AppState>((set) => ({
  agents: [],
  alerts: [],

  setAgents: (agents) => set({ agents }),

  updateAgent: (agent) =>
    set((state) => {
      const idx = state.agents.findIndex((a) => a.id === agent.id)
      if (idx >= 0) {
        const updated = [...state.agents]
        updated[idx] = { ...updated[idx], ...agent }
        return { agents: updated }
      }
      return { agents: [...state.agents, agent] }
    }),

  setAlerts: (alerts) => set({ alerts }),

  addAlert: (alert) =>
    set((state) => ({ alerts: [alert, ...state.alerts] })),

  resolveAlert: (id) =>
    set((state) => ({
      alerts: state.alerts.filter((a) => a.id !== id),
    })),
}))
```

- [ ] **Step 5: Commit**

```bash
git add frontend/src/types.ts frontend/src/api/ frontend/src/store/
git commit -m "feat: add API client, WebSocket client, and Zustand store"
```

---

### Task 16: Frontend layout + Dashboard page

**Files:**
- Create: `frontend/src/components/layout/Layout.tsx`
- Create: `frontend/src/components/agents/StatusBadge.tsx`
- Create: `frontend/src/pages/Dashboard.tsx`
- Modify: `frontend/src/App.tsx`
- Modify: `frontend/src/main.tsx`

- [ ] **Step 1: Write Layout component**

```tsx
// frontend/src/components/layout/Layout.tsx
import { Link, Outlet, useLocation } from 'react-router-dom'
import { useStore } from '../../store'

const navItems = [
  { path: '/', label: 'Dashboard' },
  { path: '/agents', label: 'Agents' },
  { path: '/configs', label: 'Configs' },
  { path: '/alerts', label: 'Alerts' },
]

export default function Layout() {
  const location = useLocation()
  const alertCount = useStore((s) => s.alerts.length)

  return (
    <div style={{ display: 'flex', minHeight: '100vh' }}>
      <nav style={{ width: 220, background: '#1a1a2e', color: '#fff', padding: '1rem' }}>
        <h2 style={{ fontSize: '1.2rem', marginBottom: '2rem' }}>otel-magnify</h2>
        <ul style={{ listStyle: 'none', padding: 0 }}>
          {navItems.map((item) => (
            <li key={item.path} style={{ marginBottom: '0.5rem' }}>
              <Link
                to={item.path}
                style={{
                  color: location.pathname === item.path ? '#4fc3f7' : '#ccc',
                  textDecoration: 'none',
                }}
              >
                {item.label}
                {item.label === 'Alerts' && alertCount > 0 && (
                  <span style={{ marginLeft: 8, background: '#e53935', borderRadius: 8, padding: '2px 6px', fontSize: '0.75rem' }}>
                    {alertCount}
                  </span>
                )}
              </Link>
            </li>
          ))}
        </ul>
      </nav>
      <main style={{ flex: 1, padding: '1.5rem', background: '#f5f5f5' }}>
        <Outlet />
      </main>
    </div>
  )
}
```

- [ ] **Step 2: Write StatusBadge component**

```tsx
// frontend/src/components/agents/StatusBadge.tsx
const colors: Record<string, string> = {
  connected: '#4caf50',
  disconnected: '#9e9e9e',
  degraded: '#ff9800',
}

export default function StatusBadge({ status }: { status: string }) {
  return (
    <span
      style={{
        display: 'inline-block',
        padding: '2px 8px',
        borderRadius: 4,
        background: colors[status] ?? '#9e9e9e',
        color: '#fff',
        fontSize: '0.8rem',
        fontWeight: 600,
      }}
    >
      {status}
    </span>
  )
}
```

- [ ] **Step 3: Write Dashboard page**

```tsx
// frontend/src/pages/Dashboard.tsx
import { useQuery } from '@tanstack/react-query'
import { agentsAPI, alertsAPI } from '../api/client'
import { useStore } from '../store'
import StatusBadge from '../components/agents/StatusBadge'

export default function Dashboard() {
  const { data: agents } = useQuery({ queryKey: ['agents'], queryFn: agentsAPI.list })
  const { data: alerts } = useQuery({ queryKey: ['alerts'], queryFn: () => alertsAPI.list(false) })

  const store = useStore()
  // Sync fetched data to store
  if (agents && agents !== store.agents) store.setAgents(agents)
  if (alerts && alerts !== store.alerts) store.setAlerts(alerts)

  const connected = agents?.filter((a) => a.status === 'connected').length ?? 0
  const total = agents?.length ?? 0

  return (
    <div>
      <h1>Dashboard</h1>
      <div style={{ display: 'flex', gap: '1rem', marginBottom: '2rem' }}>
        <StatCard label="Total Agents" value={total} />
        <StatCard label="Connected" value={connected} />
        <StatCard label="Active Alerts" value={alerts?.length ?? 0} />
      </div>
      <h2>Recent Alerts</h2>
      {alerts && alerts.length > 0 ? (
        <table style={{ width: '100%', borderCollapse: 'collapse' }}>
          <thead>
            <tr>
              <th style={th}>Agent</th><th style={th}>Rule</th><th style={th}>Severity</th><th style={th}>Message</th>
            </tr>
          </thead>
          <tbody>
            {alerts.slice(0, 5).map((a) => (
              <tr key={a.id}>
                <td style={td}>{a.agent_id}</td>
                <td style={td}>{a.rule}</td>
                <td style={td}><StatusBadge status={a.severity} /></td>
                <td style={td}>{a.message}</td>
              </tr>
            ))}
          </tbody>
        </table>
      ) : (
        <p>No active alerts.</p>
      )}
    </div>
  )
}

function StatCard({ label, value }: { label: string; value: number }) {
  return (
    <div style={{ background: '#fff', padding: '1rem 2rem', borderRadius: 8, boxShadow: '0 1px 3px rgba(0,0,0,0.1)' }}>
      <div style={{ fontSize: '2rem', fontWeight: 700 }}>{value}</div>
      <div style={{ color: '#666' }}>{label}</div>
    </div>
  )
}

const th: React.CSSProperties = { textAlign: 'left', padding: '8px', borderBottom: '2px solid #ddd' }
const td: React.CSSProperties = { padding: '8px', borderBottom: '1px solid #eee' }
```

- [ ] **Step 4: Wire App.tsx with routing**

```tsx
// frontend/src/App.tsx
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { useEffect } from 'react'
import Layout from './components/layout/Layout'
import Dashboard from './pages/Dashboard'
import { connectWS, disconnectWS } from './api/websocket'

const queryClient = new QueryClient({
  defaultOptions: { queries: { refetchOnWindowFocus: false, retry: 1 } },
})

function AppShell() {
  useEffect(() => {
    connectWS()
    return () => disconnectWS()
  }, [])

  return (
    <Routes>
      <Route element={<Layout />}>
        <Route path="/" element={<Dashboard />} />
        {/* Placeholder routes — implemented in later tasks */}
        <Route path="/agents" element={<div>Agents (coming soon)</div>} />
        <Route path="/agents/:id" element={<div>Agent Detail (coming soon)</div>} />
        <Route path="/configs" element={<div>Configs (coming soon)</div>} />
        <Route path="/alerts" element={<div>Alerts (coming soon)</div>} />
      </Route>
      <Route path="/login" element={<div>Login (coming soon)</div>} />
      <Route path="*" element={<Navigate to="/" />} />
    </Routes>
  )
}

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <AppShell />
      </BrowserRouter>
    </QueryClientProvider>
  )
}
```

- [ ] **Step 5: Update main.tsx**

```tsx
// frontend/src/main.tsx
import React from 'react'
import ReactDOM from 'react-dom/client'
import App from './App'

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>
)
```

- [ ] **Step 6: Verify it compiles**

Run: `cd frontend && npx tsc --noEmit`
Expected: no type errors.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/
git commit -m "feat: add layout, dashboard page, and routing"
```

---

### Task 17: Frontend Agents + AgentDetail pages

**Files:**
- Create: `frontend/src/components/agents/AgentCard.tsx`
- Create: `frontend/src/pages/Agents.tsx`
- Create: `frontend/src/pages/AgentDetail.tsx`
- Modify: `frontend/src/App.tsx` (replace placeholder routes)

- [ ] **Step 1: Write AgentCard component**

```tsx
// frontend/src/components/agents/AgentCard.tsx
import { Link } from 'react-router-dom'
import type { Agent } from '../../types'
import StatusBadge from './StatusBadge'

export default function AgentCard({ agent }: { agent: Agent }) {
  return (
    <Link to={`/agents/${agent.id}`} style={{ textDecoration: 'none', color: 'inherit' }}>
      <div style={{
        background: '#fff', padding: '1rem', borderRadius: 8,
        boxShadow: '0 1px 3px rgba(0,0,0,0.1)', marginBottom: '0.5rem',
        display: 'flex', justifyContent: 'space-between', alignItems: 'center',
      }}>
        <div>
          <div style={{ fontWeight: 600 }}>{agent.display_name || agent.id}</div>
          <div style={{ fontSize: '0.85rem', color: '#666' }}>
            {agent.type} &middot; v{agent.version}
          </div>
        </div>
        <StatusBadge status={agent.status} />
      </div>
    </Link>
  )
}
```

- [ ] **Step 2: Write Agents page**

```tsx
// frontend/src/pages/Agents.tsx
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { agentsAPI } from '../api/client'
import AgentCard from '../components/agents/AgentCard'

export default function Agents() {
  const { data: agents, isLoading } = useQuery({ queryKey: ['agents'], queryFn: agentsAPI.list })
  const [filterType, setFilterType] = useState<string>('')
  const [filterStatus, setFilterStatus] = useState<string>('')

  const filtered = (agents ?? []).filter((a) => {
    if (filterType && a.type !== filterType) return false
    if (filterStatus && a.status !== filterStatus) return false
    return true
  })

  return (
    <div>
      <h1>Agents</h1>
      <div style={{ display: 'flex', gap: '1rem', marginBottom: '1rem' }}>
        <select value={filterType} onChange={(e) => setFilterType(e.target.value)}>
          <option value="">All types</option>
          <option value="collector">Collector</option>
          <option value="sdk">SDK</option>
        </select>
        <select value={filterStatus} onChange={(e) => setFilterStatus(e.target.value)}>
          <option value="">All statuses</option>
          <option value="connected">Connected</option>
          <option value="disconnected">Disconnected</option>
          <option value="degraded">Degraded</option>
        </select>
      </div>
      {isLoading ? <p>Loading...</p> : filtered.map((a) => <AgentCard key={a.id} agent={a} />)}
    </div>
  )
}
```

- [ ] **Step 3: Write AgentDetail page**

```tsx
// frontend/src/pages/AgentDetail.tsx
import { useParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { agentsAPI } from '../api/client'
import StatusBadge from '../components/agents/StatusBadge'

export default function AgentDetail() {
  const { id } = useParams<{ id: string }>()
  const { data: agent, isLoading } = useQuery({
    queryKey: ['agent', id],
    queryFn: () => agentsAPI.get(id!),
    enabled: !!id,
  })

  if (isLoading) return <p>Loading...</p>
  if (!agent) return <p>Agent not found.</p>

  return (
    <div>
      <h1>{agent.display_name || agent.id}</h1>
      <div style={{ display: 'flex', gap: '2rem', marginBottom: '1rem' }}>
        <div><strong>Type:</strong> {agent.type}</div>
        <div><strong>Version:</strong> {agent.version}</div>
        <div><strong>Status:</strong> <StatusBadge status={agent.status} /></div>
        <div><strong>Last seen:</strong> {new Date(agent.last_seen_at).toLocaleString()}</div>
      </div>
      {Object.keys(agent.labels).length > 0 && (
        <div style={{ marginBottom: '1rem' }}>
          <strong>Labels:</strong>
          <div style={{ display: 'flex', gap: '0.5rem', flexWrap: 'wrap', marginTop: '0.5rem' }}>
            {Object.entries(agent.labels).map(([k, v]) => (
              <span key={k} style={{ background: '#e3f2fd', padding: '2px 8px', borderRadius: 4, fontSize: '0.85rem' }}>
                {k}={v}
              </span>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}
```

- [ ] **Step 4: Update App.tsx routes**

Replace the placeholder routes in `App.tsx`:

```tsx
import Agents from './pages/Agents'
import AgentDetail from './pages/AgentDetail'
```

Replace route lines:
```tsx
<Route path="/agents" element={<Agents />} />
<Route path="/agents/:id" element={<AgentDetail />} />
```

- [ ] **Step 5: Verify compilation**

Run: `cd frontend && npx tsc --noEmit`
Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/
git commit -m "feat: add Agents list and AgentDetail pages"
```

---

### Task 18: Frontend Configs + Alerts + Login pages

**Files:**
- Create: `frontend/src/components/config/YamlEditor.tsx`
- Create: `frontend/src/pages/Configs.tsx`
- Create: `frontend/src/pages/Alerts.tsx`
- Create: `frontend/src/pages/Login.tsx`
- Modify: `frontend/src/App.tsx` (replace remaining placeholders)

- [ ] **Step 1: Write YamlEditor component**

```tsx
// frontend/src/components/config/YamlEditor.tsx
import { useRef, useEffect } from 'react'
import { EditorView, basicSetup } from 'codemirror'
import { yaml } from '@codemirror/lang-yaml'
import { EditorState } from '@codemirror/state'

interface Props {
  value: string
  onChange?: (value: string) => void
  readOnly?: boolean
}

export default function YamlEditor({ value, onChange, readOnly = false }: Props) {
  const ref = useRef<HTMLDivElement>(null)
  const viewRef = useRef<EditorView | null>(null)

  useEffect(() => {
    if (!ref.current) return

    const extensions = [
      basicSetup,
      yaml(),
      EditorView.theme({ '&': { height: '400px', border: '1px solid #ccc', borderRadius: '4px' } }),
    ]

    if (readOnly) {
      extensions.push(EditorState.readOnly.of(true))
    }

    if (onChange) {
      extensions.push(
        EditorView.updateListener.of((update) => {
          if (update.docChanged) {
            onChange(update.state.doc.toString())
          }
        })
      )
    }

    const view = new EditorView({
      state: EditorState.create({ doc: value, extensions }),
      parent: ref.current,
    })
    viewRef.current = view

    return () => view.destroy()
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  return <div ref={ref} />
}
```

- [ ] **Step 2: Write Configs page**

```tsx
// frontend/src/pages/Configs.tsx
import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { configsAPI } from '../api/client'
import YamlEditor from '../components/config/YamlEditor'

export default function Configs() {
  const queryClient = useQueryClient()
  const { data: configs, isLoading } = useQuery({ queryKey: ['configs'], queryFn: configsAPI.list })

  const [name, setName] = useState('')
  const [content, setContent] = useState('')
  const [showForm, setShowForm] = useState(false)

  const createMutation = useMutation({
    mutationFn: () => configsAPI.create(name, content),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['configs'] })
      setName('')
      setContent('')
      setShowForm(false)
    },
  })

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <h1>Configs</h1>
        <button onClick={() => setShowForm(!showForm)}>
          {showForm ? 'Cancel' : 'New Config'}
        </button>
      </div>

      {showForm && (
        <div style={{ background: '#fff', padding: '1rem', borderRadius: 8, marginBottom: '1rem' }}>
          <input
            placeholder="Config name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            style={{ width: '100%', marginBottom: '0.5rem', padding: '0.5rem' }}
          />
          <YamlEditor value={content} onChange={setContent} />
          <button
            onClick={() => createMutation.mutate()}
            disabled={!name || !content || createMutation.isPending}
            style={{ marginTop: '0.5rem' }}
          >
            {createMutation.isPending ? 'Creating...' : 'Create'}
          </button>
        </div>
      )}

      {isLoading ? (
        <p>Loading...</p>
      ) : (
        <table style={{ width: '100%', borderCollapse: 'collapse', background: '#fff' }}>
          <thead>
            <tr>
              <th style={th}>Name</th><th style={th}>Created by</th><th style={th}>Created at</th><th style={th}>ID (hash)</th>
            </tr>
          </thead>
          <tbody>
            {(configs ?? []).map((c) => (
              <tr key={c.id}>
                <td style={td}>{c.name}</td>
                <td style={td}>{c.created_by}</td>
                <td style={td}>{new Date(c.created_at).toLocaleString()}</td>
                <td style={td}><code>{c.id.substring(0, 12)}...</code></td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}

const th: React.CSSProperties = { textAlign: 'left', padding: '8px', borderBottom: '2px solid #ddd' }
const td: React.CSSProperties = { padding: '8px', borderBottom: '1px solid #eee' }
```

- [ ] **Step 3: Write Alerts page**

```tsx
// frontend/src/pages/Alerts.tsx
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { alertsAPI } from '../api/client'
import StatusBadge from '../components/agents/StatusBadge'

export default function Alerts() {
  const queryClient = useQueryClient()
  const { data: alerts, isLoading } = useQuery({ queryKey: ['alerts'], queryFn: () => alertsAPI.list(false) })

  const resolveMutation = useMutation({
    mutationFn: (id: string) => alertsAPI.resolve(id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['alerts'] }),
  })

  return (
    <div>
      <h1>Alerts</h1>
      {isLoading ? (
        <p>Loading...</p>
      ) : (alerts ?? []).length === 0 ? (
        <p>No active alerts.</p>
      ) : (
        <table style={{ width: '100%', borderCollapse: 'collapse', background: '#fff' }}>
          <thead>
            <tr>
              <th style={th}>Agent</th><th style={th}>Rule</th><th style={th}>Severity</th>
              <th style={th}>Message</th><th style={th}>Fired at</th><th style={th}>Action</th>
            </tr>
          </thead>
          <tbody>
            {(alerts ?? []).map((a) => (
              <tr key={a.id}>
                <td style={td}>{a.agent_id}</td>
                <td style={td}>{a.rule}</td>
                <td style={td}><StatusBadge status={a.severity} /></td>
                <td style={td}>{a.message}</td>
                <td style={td}>{new Date(a.fired_at).toLocaleString()}</td>
                <td style={td}>
                  <button onClick={() => resolveMutation.mutate(a.id)} disabled={resolveMutation.isPending}>
                    Resolve
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}

const th: React.CSSProperties = { textAlign: 'left', padding: '8px', borderBottom: '2px solid #ddd' }
const td: React.CSSProperties = { padding: '8px', borderBottom: '1px solid #eee' }
```

- [ ] **Step 4: Write Login page**

```tsx
// frontend/src/pages/Login.tsx
import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { authAPI } from '../api/client'

export default function Login() {
  const navigate = useNavigate()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    setLoading(true)
    try {
      const { token } = await authAPI.login(email, password)
      localStorage.setItem('token', token)
      navigate('/')
    } catch {
      setError('Invalid credentials')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', minHeight: '100vh', background: '#1a1a2e' }}>
      <form onSubmit={handleSubmit} style={{ background: '#fff', padding: '2rem', borderRadius: 8, width: 360 }}>
        <h1 style={{ marginBottom: '1.5rem', textAlign: 'center' }}>otel-magnify</h1>
        {error && <div style={{ color: '#e53935', marginBottom: '1rem' }}>{error}</div>}
        <input
          type="email" placeholder="Email" value={email}
          onChange={(e) => setEmail(e.target.value)}
          style={{ width: '100%', padding: '0.5rem', marginBottom: '1rem', boxSizing: 'border-box' }}
        />
        <input
          type="password" placeholder="Password" value={password}
          onChange={(e) => setPassword(e.target.value)}
          style={{ width: '100%', padding: '0.5rem', marginBottom: '1rem', boxSizing: 'border-box' }}
        />
        <button type="submit" disabled={loading} style={{ width: '100%', padding: '0.5rem' }}>
          {loading ? 'Signing in...' : 'Sign in'}
        </button>
      </form>
    </div>
  )
}
```

- [ ] **Step 5: Update App.tsx — replace all remaining placeholders**

Import the new pages and update the routes:

```tsx
import Configs from './pages/Configs'
import Alerts from './pages/Alerts'
import Login from './pages/Login'
```

Replace route lines:
```tsx
<Route path="/configs" element={<Configs />} />
<Route path="/alerts" element={<Alerts />} />
```

And the login route:
```tsx
<Route path="/login" element={<Login />} />
```

- [ ] **Step 6: Verify compilation**

Run: `cd frontend && npx tsc --noEmit`
Expected: no errors.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/
git commit -m "feat: add Configs, Alerts, and Login pages with YAML editor"
```

---

## Phase 4: Deployment

### Task 19: Dockerfile + docker-compose

**Files:**
- Create: `Dockerfile`
- Create: `docker-compose.yml`

- [ ] **Step 1: Write Dockerfile**

```dockerfile
# Dockerfile
FROM node:20-alpine AS frontend-build
WORKDIR /app/frontend
COPY frontend/package*.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM golang:1.22-alpine AS backend-build
WORKDIR /app/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
# Embed frontend dist into the binary
COPY --from=frontend-build /app/frontend/dist ./cmd/server/dist
RUN CGO_ENABLED=0 go build -o /otel-magnify ./cmd/server/

FROM alpine:3.19
RUN apk add --no-cache ca-certificates
COPY --from=backend-build /otel-magnify /usr/local/bin/otel-magnify
EXPOSE 8080 4320
ENTRYPOINT ["otel-magnify"]
```

Note: The `embed.FS` for frontend assets requires adding the following to `main.go` (modify in a future task if static file serving is not yet wired):

```go
//go:embed dist
var frontendDist embed.FS
```

For now, the React build output is placed next to the binary. Static file serving will be added to the chi router to serve `dist/` at `/`.

- [ ] **Step 2: Write docker-compose.yml**

```yaml
# docker-compose.yml
services:
  otel-magnify:
    build: .
    ports:
      - "8080:8080"
      - "4320:4320"
    environment:
      DB_DRIVER: sqlite
      DB_DSN: /data/otel-magnify.db
      JWT_SECRET: ${JWT_SECRET:-change-me-in-production}
      LISTEN_ADDR: ":8080"
      OPAMP_ADDR: ":4320"
      CORS_ORIGINS: "http://localhost:8080"
    volumes:
      - magnify-data:/data

volumes:
  magnify-data:
```

- [ ] **Step 3: Verify docker build**

Run: `docker compose build`
Expected: build completes successfully.

- [ ] **Step 4: Commit**

```bash
git add Dockerfile docker-compose.yml
git commit -m "feat: add Dockerfile and docker-compose for deployment"
```

---

### Task 20: Helm chart

**Files:**
- Create: `helm/otel-magnify/Chart.yaml`
- Create: `helm/otel-magnify/values.yaml`
- Create: `helm/otel-magnify/templates/deployment.yaml`
- Create: `helm/otel-magnify/templates/service.yaml`
- Create: `helm/otel-magnify/templates/ingress.yaml`
- Create: `helm/otel-magnify/templates/secret.yaml`

- [ ] **Step 1: Write Chart.yaml**

```yaml
# helm/otel-magnify/Chart.yaml
apiVersion: v2
name: otel-magnify
description: Centralized OpenTelemetry agent management via OpAMP
version: 0.1.0
appVersion: "0.1.0"
```

- [ ] **Step 2: Write values.yaml**

```yaml
# helm/otel-magnify/values.yaml
replicaCount: 1

image:
  repository: otel-magnify
  tag: latest
  pullPolicy: IfNotPresent

service:
  type: ClusterIP
  apiPort: 8080
  opampPort: 4320

ingress:
  enabled: false
  className: ""
  hosts:
    - host: otel-magnify.local
      paths:
        - path: /
          pathType: Prefix
  tls: []

config:
  dbDriver: pgx
  dbDSN: ""  # Set via secret or external config
  corsOrigins: ""

jwtSecret: ""  # Set during install: --set jwtSecret=xxx

resources:
  limits:
    memory: 256Mi
  requests:
    cpu: 100m
    memory: 128Mi
```

- [ ] **Step 3: Write templates**

```yaml
# helm/otel-magnify/templates/secret.yaml
apiVersion: v1
kind: Secret
metadata:
  name: {{ .Release.Name }}-secret
type: Opaque
stringData:
  jwt-secret: {{ .Values.jwtSecret | quote }}
  db-dsn: {{ .Values.config.dbDSN | quote }}
```

```yaml
# helm/otel-magnify/templates/deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ .Release.Name }}
  labels:
    app: {{ .Release.Name }}
spec:
  replicas: {{ .Values.replicaCount }}
  selector:
    matchLabels:
      app: {{ .Release.Name }}
  template:
    metadata:
      labels:
        app: {{ .Release.Name }}
    spec:
      containers:
        - name: otel-magnify
          image: "{{ .Values.image.repository }}:{{ .Values.image.tag }}"
          imagePullPolicy: {{ .Values.image.pullPolicy }}
          ports:
            - name: api
              containerPort: 8080
            - name: opamp
              containerPort: 4320
          env:
            - name: DB_DRIVER
              value: {{ .Values.config.dbDriver | quote }}
            - name: DB_DSN
              valueFrom:
                secretKeyRef:
                  name: {{ .Release.Name }}-secret
                  key: db-dsn
            - name: JWT_SECRET
              valueFrom:
                secretKeyRef:
                  name: {{ .Release.Name }}-secret
                  key: jwt-secret
            - name: LISTEN_ADDR
              value: ":8080"
            - name: OPAMP_ADDR
              value: ":4320"
            - name: CORS_ORIGINS
              value: {{ .Values.config.corsOrigins | quote }}
          resources:
            {{- toYaml .Values.resources | nindent 12 }}
          livenessProbe:
            httpGet:
              path: /api/agents
              port: api
            initialDelaySeconds: 5
          readinessProbe:
            httpGet:
              path: /api/agents
              port: api
            initialDelaySeconds: 3
```

```yaml
# helm/otel-magnify/templates/service.yaml
apiVersion: v1
kind: Service
metadata:
  name: {{ .Release.Name }}
spec:
  type: {{ .Values.service.type }}
  ports:
    - name: api
      port: {{ .Values.service.apiPort }}
      targetPort: api
    - name: opamp
      port: {{ .Values.service.opampPort }}
      targetPort: opamp
  selector:
    app: {{ .Release.Name }}
```

```yaml
# helm/otel-magnify/templates/ingress.yaml
{{- if .Values.ingress.enabled }}
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: {{ .Release.Name }}
  {{- if .Values.ingress.className }}
  annotations:
    kubernetes.io/ingress.class: {{ .Values.ingress.className }}
  {{- end }}
spec:
  {{- if .Values.ingress.tls }}
  tls:
    {{- toYaml .Values.ingress.tls | nindent 4 }}
  {{- end }}
  rules:
    {{- range .Values.ingress.hosts }}
    - host: {{ .host }}
      http:
        paths:
          {{- range .paths }}
          - path: {{ .path }}
            pathType: {{ .pathType }}
            backend:
              service:
                name: {{ $.Release.Name }}
                port:
                  name: api
          {{- end }}
    {{- end }}
{{- end }}
```

- [ ] **Step 4: Validate chart**

Run: `helm lint helm/otel-magnify/`
Expected: `1 chart(s) linted, 0 chart(s) failed`

- [ ] **Step 5: Commit**

```bash
git add helm/
git commit -m "feat: add Helm chart for Kubernetes deployment"
```

---

## Post-implementation Notes

- **Frontend embed.FS**: After Task 19, wire `embed.FS` in `main.go` to serve the React build from the Go binary. Add a `fileServer` handler at `/` that serves `dist/` and falls back to `index.html` for SPA routing.
- **CORS middleware**: Add `go-chi/cors` middleware to `router.go` using `cfg.CORSOrigins`.
- **Seed admin user**: Add a CLI command or startup flag to create the first admin user (e.g. `otel-magnify seed --email admin@local --password xxx`).
- **config_drift and version_outdated rules**: Extend the alert engine with these rules once the "expected config" concept is established in the UI.
