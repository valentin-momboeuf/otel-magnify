# Design — otel-magnify: OpAMP Management Platform

**Date:** 2026-04-13
**Status:** approved

## Context

`otel-magnify` is a web application providing centralized management of OpenTelemetry agents via the OpAMP (Open Agent Management Protocol). It targets two agent types: OpenTelemetry Collectors and SDK agents (Java, Python, Go). The primary use cases are: observing agent state, remotely managing configurations, and alerting on drift or failures.

## Scope

- **Phase 1**: small team (~100 agents), simple JWT auth, SQLite
- **Phase 2**: multi-tenant, role/org-based auth, PostgreSQL, thousands of agents

This document covers phase 1 with extension hooks planned for phase 2.

## Architecture Overview

```
┌─────────────────────────────────────────────────────┐
│                   otel-magnify                      │
│                                                     │
│  ┌──────────────┐    ┌──────────────────────────┐  │
│  │  React/Vite  │◄──►│     Go Backend           │  │
│  │  (frontend)  │    │  ┌────────────────────┐  │  │
│  │              │    │  │  OpAMP Server      │  │  │
│  │  REST + WS   │    │  │  (opamp-go lib)    │  │  │
│  └──────────────┘    │  └────────┬───────────┘  │  │
│                      │           │               │  │
│                      │  ┌────────▼───────────┐  │  │
│                      │  │  REST API + WS hub │  │  │
│                      │  └────────┬───────────┘  │  │
│                      │           │               │  │
│                      │  ┌────────▼───────────┐  │  │
│                      │  │  SQLite / Postgres  │  │  │
│                      │  └────────────────────┘  │  │
│                      └──────────────────────────┘  │
└─────────────────────────────────────────────────────┘
         ▲                         ▲
         │ WebSocket OpAMP         │ WebSocket OpAMP
    OTel Collectors          SDK Agents (Java/Python/Go)
```

## Go Backend

### Structure

```
backend/
├── cmd/server/          # entrypoint, config via env vars or YAML file
├── internal/
│   ├── opamp/           # OpAMP server: agent connections, heartbeats, config push
│   ├── store/           # DB access + migrations (golang-migrate)
│   ├── api/             # REST handlers + WebSocket hub to frontend
│   ├── alerts/          # alert rules engine (evaluated every 30s)
│   └── auth/            # JWT middleware (phase 1), multi-tenant hooks (phase 2)
└── pkg/
    └── models/          # shared structs: Agent, Config, Alert, User
```

### Key Dependencies

| Dependency | Usage |
|---|---|
| `open-telemetry/opamp-go` | OpAMP server SDK |
| `go-chi/chi` | Idiomatic HTTP router, `net/http` compatible |
| `golang-migrate/migrate` | Versioned DB migrations (SQL up/down files) |
| `golang-jwt/jwt` | JWT token generation and validation (HS256) |

### Main Flow

1. An agent connects via OpAMP WebSocket → `opamp/` registers it in the store
2. On each heartbeat, `opamp/` updates the agent status and active config in DB
3. `alerts/` evaluates rules every 30s and creates alerts as needed
4. `api/` fans out changes to the frontend in real-time via WebSocket
5. User modifies a config in the UI → `api/` calls `opamp/` which pushes the config to the target agent

### Authentication

- **Phase 1**: login/password stored in DB, JWT signed with HS256, `Authorization: Bearer <token>` header
- **Phase 2**: JWT claims extended with `tenant_id`, automatic per-tenant data filtering in every handler

## React Frontend

### Structure

```
frontend/
├── src/
│   ├── api/          # REST clients (axios) + native WebSocket
│   ├── components/
│   │   ├── agents/   # agent list, agent card, status badge
│   │   ├── config/   # YAML editor (CodeMirror 6), before/after diff on push
│   │   ├── alerts/   # alerts panel, rule configuration
│   │   └── layout/   # navbar, sidebar, global shell
│   ├── pages/
│   │   ├── Dashboard.tsx    # overview: active agents, recent alerts
│   │   ├── Agents.tsx       # list + filters (type, status, version, labels)
│   │   ├── AgentDetail.tsx  # detailed state, active config, history
│   │   ├── Configs.tsx      # config templates, versioning
│   │   └── Alerts.tsx       # rules + alert history
│   └── store/        # global state (Zustand)
```

### Key Dependencies

| Dependency | Usage |
|---|---|
| `Vite` | Build tool and dev server |
| `Recharts` | Charts (active agents over time, heartbeat latency) |
| `CodeMirror 6` | YAML editor with syntax highlighting |
| `Zustand` | Lightweight state management |
| `TanStack Query` | REST fetching and caching |

### Real-time Updates

Backend WebSocket → Zustand store update → automatic React re-render. Every agent state change (connect, disconnect, config applied) is pushed immediately without polling.

## Data Model

```sql
-- Agents registered via OpAMP
agents (
  id               TEXT PRIMARY KEY,   -- OpAMP agent_id (UUID)
  display_name     TEXT,
  type             TEXT,               -- "collector" | "sdk"
  version          TEXT,
  status           TEXT,               -- "connected" | "disconnected" | "degraded"
  last_seen_at     TIMESTAMP,
  labels           JSONB,              -- e.g. {"env": "prod", "region": "eu-west-1"}
  active_config_id TEXT REFERENCES configs(id)
)

-- Configs versioned by content hash
configs (
  id          TEXT PRIMARY KEY,        -- SHA256 of YAML content
  name        TEXT,
  content     TEXT,                    -- raw YAML
  created_at  TIMESTAMP,
  created_by  TEXT
)

-- History of configs applied per agent
agent_configs (
  agent_id    TEXT REFERENCES agents(id),
  config_id   TEXT REFERENCES configs(id),
  applied_at  TIMESTAMP,
  status      TEXT                     -- "pending" | "applied" | "failed"
)

-- Alerts
alerts (
  id          TEXT PRIMARY KEY,
  agent_id    TEXT REFERENCES agents(id),
  rule        TEXT,                    -- "agent_down" | "config_drift" | "version_outdated"
  severity    TEXT,                    -- "warning" | "critical"
  message     TEXT,
  fired_at    TIMESTAMP,
  resolved_at TIMESTAMP
)

-- Users
users (
  id            TEXT PRIMARY KEY,
  email         TEXT UNIQUE,
  password_hash TEXT,
  role          TEXT,                  -- "admin" | "viewer"
  tenant_id     TEXT                   -- NULL in phase 1, used in phase 2
)
```

## Alert Engine

Rules evaluated every 30 seconds. Thresholds and activation configurable via the UI.

| Rule | Condition | Severity |
|---|---|---|
| `agent_down` | `last_seen_at` > 5 minutes | critical |
| `config_drift` | active config ≠ expected config | warning |
| `version_outdated` | version < defined minimum version | warning |

**Phase 1 notifications:** configurable HTTP webhook.
**Phase 2 notifications:** email.

## Deployment

| Environment | Method | DB |
|---|---|---|
| Local (dev) | `go run` + `vite dev` | SQLite |
| Docker Compose | `docker-compose up` | SQLite or Postgres |
| Kubernetes | Helm chart | Postgres |

Multi-stage Dockerfile: React build → assets embedded in the Go binary via `embed.FS` → final image ~20MB, single container to operate.

## Security

The [`security-guidance`](https://github.com/anthropics/claude-code/tree/main/plugins/security-guidance) plugin is enabled during development. It proactively warns about vulnerable patterns (command injection, XSS, GitHub Actions, etc.) on every file edit.

Project-specific security requirements:
- **JWT secret**: injected via environment variable, never hardcoded
- **OpAMP config validation**: payloads received from agents are validated before persistence
- **CORS**: allowed origins explicitly configured (no wildcard in production)
- **TLS**: mandatory TLS termination in production (K8s Ingress or reverse proxy)
- **Passwords**: hashed with bcrypt (cost ≥ 12)
