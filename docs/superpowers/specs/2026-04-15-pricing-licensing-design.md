# Pricing & Licensing Model — otel-magnify

**Date:** 2026-04-15
**Status:** Draft
**Supersedes:** Previous per-agent pricing model (2026-04-14)

---

## 1. Licensing model

### Two repositories

| Repo | Visibility | License |
|------|-----------|---------|
| `otel-magnify` | Public (GitHub) | Apache 2.0 |
| `otel-magnify-enterprise` | Private (GitHub) | Proprietary |

The community repo is the full, standalone product with no agent limits. The enterprise repo is a Go module overlay that imports the community module and composes an enriched binary.

### Tier activation via license file

A license file (loaded at startup) determines the active tier:

- **No license file** → Community (all community features, no restrictions)
- **Pro license file** → Community + Pro features
- **Enterprise license file** → Community + Pro + Enterprise features

Features not covered by the license are never loaded at runtime (handlers not mounted, middleware not registered).

---

## 2. Pricing

### Grid

| | Community | Pro | Enterprise |
|---|---|---|---|
| Monthly price | Free | **EUR 699** | **From EUR 1,999** |
| Annual price | -- | EUR 629/mo (EUR 7,548/yr) | On quote (-10% annual) |
| Agents | Unlimited | Unlimited | Unlimited |
| Support | Community (GitHub) | < 24h | < 4h critical |
| Distribution | Public download | License file | License file |

### Annual commitment

10% discount on annual commitment for both Pro and Enterprise tiers.

### Design Partner program

50% discount for the first 5 paying customers in year one to build reference logos.

### Competitive positioning

| Solution | Model | Indicative price | vs otel-magnify |
|----------|-------|-----------------|-----------------|
| BindPlane (ObservIQ) | Per-agent | ~$15/agent/mo | Pro break-even at ~47 agents; cheaper beyond |
| Datadog | Per-host (full stack) | ~$23/host/mo | Not directly comparable; otel-magnify is vendor-neutral |
| Grafana Cloud | Per-metric/log | Variable | No native fleet management |

**Key differentiators:**
- Flat-rate pricing: predictable costs, no billing surprises
- Vendor-neutral: no backend lock-in
- Self-hosted first: critical for regulated sectors (energy, defense, healthcare)

---

## 3. Feature matrix

### Community (Apache 2.0, public repo)

- Agent inventory with real-time status (OpAMP)
- Config push to agents
- Basic alerting (webhook)
- JWT auth (single admin role)
- Embedded SQLite
- Helm chart
- Full frontend (Signal Deck)
- REST API + WebSocket
- Unlimited agents

### Pro (Proprietary, Pro license file)

Everything in Community, plus:

- PostgreSQL support
- Config rollback / versioning
- Canary config push (progressive deployment)
- Email notifications for alerts
- Support < 24h

### Enterprise (Proprietary, Enterprise license file)

Everything in Pro, plus:

- SSO (SAML 2.0 / OIDC)
- Granular RBAC (custom roles, per-resource permissions)
- Full audit log (who did what, when)
- Multi-tenancy (per-tenant isolation)
- HA clustering
- Support < 4h critical, dedicated Slack/Teams channel, quarterly architecture review

---

## 4. Technical architecture — module overlay

### Enterprise repo structure

```
otel-magnify-enterprise/
├── cmd/server/main.go        # Entrypoint: composes community + pro/enterprise
├── internal/
│   ├── license/              # License file reading and validation
│   ├── sso/                  # SAML 2.0 / OIDC providers
│   ├── rbac/                 # Granular RBAC engine
│   ├── audit/                # Audit log engine
│   ├── tenant/               # Multi-tenant isolation
│   ├── ha/                   # Clustering / leader election
│   ├── config/               # Rollback, versioning, canary push
│   ├── notify/               # Email notifier
│   └── store/                # PostgreSQL driver
├── pkg/license/              # Shared license types
├── go.mod                    # require github.com/valentin-momboeuf/otel-magnify
└── LICENSE                   # Proprietary
```

### Startup composition flow (cmd/server/main.go)

1. Load license file → determine tier (Pro or Enterprise)
2. Initialize the community server via import of the public module
3. Register additional providers based on tier:
   - **Pro:** `StoreDriver(postgres)`, `ConfigPolicy(rollback, canary)`, `AlertNotifier(email)`
   - **Enterprise:** all Pro + `AuthProvider(sso)`, `RBACEngine`, `AuditLogger`, `TenantIsolation`, `HACluster`
4. Features not covered by the license are never loaded (no dead code at runtime)

### Community repo — extensibility interfaces

The `otel-magnify` repo must define extension interfaces in `pkg/extensibility/` (or similar) to allow the enterprise module to plug in without modifying community code. Interfaces to define:

- `AuthProvider` — pluggable authentication (JWT default, SSO in enterprise)
- `StoreDriver` — pluggable database backend (SQLite default, PostgreSQL in pro)
- `AlertNotifier` — pluggable notification channels (webhook default, email in pro)
- `ConfigPolicy` — pluggable config deployment strategies (direct push default, rollback/canary in pro)
- `AuditLogger` — pluggable audit backend (no-op default, full log in enterprise)
- `RBACEngine` — pluggable authorization (single-role default, granular in enterprise)
- `TenantIsolation` — pluggable tenant context (single-tenant default, multi in enterprise)

---

## 5. Launch strategy

1. **Phase 1** (first 6 months): Community (free, public) + Enterprise (on quote, private). Focus on Enedis pilot.
2. **Phase 2**: Add Pro tier to capture mid-market.
3. **Phase 3**: Managed cloud offering (SaaS) — higher margins, broader market.

Pricing to be recalibrated after Enedis pilot based on perceived value feedback.

---

## 6. Key changes from previous model

| Aspect | Previous (2026-04-14) | New (2026-04-15) |
|--------|----------------------|-------------------|
| Agent limits | Community capped at 20 | No limits on any tier |
| Pricing model | Per-agent, degressive | Flat-rate per tier |
| Repos | Single repo | Two repos (public + private) |
| Tiers | Community / Pro / Enterprise / Custom | Community / Pro / Enterprise |
| Pro price | EUR 500-1,500/mo (variable) | EUR 699/mo (fixed) |
| Enterprise price | EUR 3,000-8,000/mo (hidden) | From EUR 1,999/mo (displayed) |
| License | Undecided (Apache 2.0 vs BSL 1.1) | Apache 2.0 (community) + Proprietary (enterprise) |
| Architecture | Single binary | Module overlay (Go import) |
