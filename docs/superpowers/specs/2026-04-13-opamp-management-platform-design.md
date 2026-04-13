# Design — otel-magnify : plateforme de gestion OpAMP

**Date :** 2026-04-13
**Statut :** approuvé

## Contexte

`otel-magnify` est une application web permettant une gestion centralisée des agents OpenTelemetry via le protocole OpAMP (Open Agent Management Protocol). Elle cible deux types d'agents : les OpenTelemetry Collectors et les agents SDK (Java, Python, Go). Le cas d'usage principal est triple : observer l'état des agents, piloter leurs configurations à distance, et être alerté en cas de dérive ou de panne.

## Périmètre

- **Phase 1** : équipe restreinte (~100 agents), auth JWT simple, SQLite
- **Phase 2** : multi-tenant, auth par rôle/organisation, PostgreSQL, milliers d'agents

Ce document couvre la phase 1 avec les hooks d'extension prévus pour la phase 2.

## Architecture générale

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

## Backend Go

### Structure

```
backend/
├── cmd/server/          # entrypoint, config via env vars ou fichier YAML
├── internal/
│   ├── opamp/           # serveur OpAMP : connexions agents, heartbeats, push config
│   ├── store/           # accès DB + migrations (golang-migrate)
│   ├── api/             # handlers REST + WebSocket hub vers le frontend
│   ├── alerts/          # moteur de règles d'alertes (évaluation toutes les 30s)
│   └── auth/            # middleware JWT (phase 1), hooks multi-tenant (phase 2)
└── pkg/
    └── models/          # structs partagés : Agent, Config, Alert, User
```

### Dépendances clés

| Dépendance | Usage |
|---|---|
| `open-telemetry/opamp-go` | SDK OpAMP serveur |
| `go-chi/chi` | Routeur HTTP idiomatique, compatible `net/http` |
| `golang-migrate/migrate` | Migrations DB versionnées (fichiers SQL up/down) |
| `golang-jwt/jwt` | Génération et validation de tokens JWT (HS256) |

### Flux principal

1. Un agent se connecte via WebSocket OpAMP → `opamp/` l'enregistre dans le store
2. À chaque heartbeat, `opamp/` met à jour le statut et la config active en DB
3. `alerts/` évalue les règles toutes les 30s et crée des alertes si nécessaire
4. `api/` diffuse les changements en temps-réel au frontend via WebSocket (fan-out)
5. L'utilisateur modifie une config via l'UI → `api/` appelle `opamp/` qui push la config à l'agent cible

### Authentification

- **Phase 1** : login/password en DB, JWT signé HS256, header `Authorization: Bearer <token>`
- **Phase 2** : claims JWT étendus avec `tenant_id`, filtrage automatique des données par tenant dans chaque handler

## Frontend React

### Structure

```
frontend/
├── src/
│   ├── api/          # clients REST (axios) + WebSocket natif
│   ├── components/
│   │   ├── agents/   # liste, carte agent, badge statut
│   │   ├── config/   # éditeur YAML (CodeMirror 6), diff avant/après push
│   │   ├── alerts/   # panneau alertes, configuration des règles
│   │   └── layout/   # navbar, sidebar, shell global
│   ├── pages/
│   │   ├── Dashboard.tsx    # vue d'ensemble : agents actifs, alertes récentes
│   │   ├── Agents.tsx       # liste + filtres (type, statut, version, labels)
│   │   ├── AgentDetail.tsx  # état détaillé, config active, historique
│   │   ├── Configs.tsx      # templates de config, versioning
│   │   └── Alerts.tsx       # règles + historique des alertes
│   └── store/        # état global Zustand
```

### Dépendances clés

| Dépendance | Usage |
|---|---|
| `Vite` | Build et dev server |
| `Recharts` | Graphiques (agents actifs, latence heartbeat) |
| `CodeMirror 6` | Éditeur YAML avec syntax highlighting |
| `Zustand` | State management léger |
| `TanStack Query` | Fetching REST + cache |

### Temps-réel

WebSocket backend → mise à jour du store Zustand → re-render React automatique. Chaque changement d'état d'agent (connexion, déconnexion, config appliquée) est diffusé immédiatement sans polling.

## Modèle de données

```sql
-- Agents enregistrés via OpAMP
agents (
  id              TEXT PRIMARY KEY,   -- agent_id OpAMP (UUID)
  display_name    TEXT,
  type            TEXT,               -- "collector" | "sdk"
  version         TEXT,
  status          TEXT,               -- "connected" | "disconnected" | "degraded"
  last_seen_at    TIMESTAMP,
  labels          JSONB,              -- ex: {"env": "prod", "region": "eu-west-1"}
  active_config_id TEXT REFERENCES configs(id)
)

-- Configs versionnées par hash de contenu
configs (
  id          TEXT PRIMARY KEY,       -- SHA256 du contenu YAML
  name        TEXT,
  content     TEXT,                   -- YAML brut
  created_at  TIMESTAMP,
  created_by  TEXT
)

-- Historique des configs appliquées par agent
agent_configs (
  agent_id    TEXT REFERENCES agents(id),
  config_id   TEXT REFERENCES configs(id),
  applied_at  TIMESTAMP,
  status      TEXT                    -- "pending" | "applied" | "failed"
)

-- Alertes
alerts (
  id          TEXT PRIMARY KEY,
  agent_id    TEXT REFERENCES agents(id),
  rule        TEXT,                   -- "agent_down" | "config_drift" | "version_outdated"
  severity    TEXT,                   -- "warning" | "critical"
  message     TEXT,
  fired_at    TIMESTAMP,
  resolved_at TIMESTAMP
)

-- Utilisateurs
users (
  id            TEXT PRIMARY KEY,
  email         TEXT UNIQUE,
  password_hash TEXT,
  role          TEXT,                 -- "admin" | "viewer"
  tenant_id     TEXT                  -- NULL en phase 1, utilisé en phase 2
)
```

## Moteur d'alertes

Évaluation toutes les 30 secondes. Règles configurables via l'UI (seuils, activation).

| Règle | Condition | Sévérité |
|---|---|---|
| `agent_down` | `last_seen_at` > 5 minutes | critical |
| `config_drift` | config active ≠ config attendue | warning |
| `version_outdated` | version < version minimale définie | warning |

**Notifications phase 1 :** webhook HTTP configurable.
**Notifications phase 2 :** email.

## Déploiement

| Environnement | Méthode | DB |
|---|---|---|
| Local (dev) | `go run` + `vite dev` | SQLite |
| Docker Compose | `docker-compose up` | SQLite ou Postgres |
| Kubernetes | Helm chart | Postgres |

Build : Dockerfile multi-stage — build React → assets embarqués dans le binaire Go via `embed.FS` → image finale ~20MB, un seul conteneur à opérer.

## Sécurité

Le plugin [`security-guidance`](https://github.com/anthropics/claude-code/tree/main/plugins/security-guidance) est activé pendant le développement. Il avertit proactivement des patterns vulnérables (injection de commande, XSS, GitHub Actions, etc.) à chaque édition de fichier.

Points d'attention spécifiques à ce projet :
- **JWT secret** : injecté par variable d'environnement, jamais hardcodé
- **Validation des configs OpAMP** : les payloads reçus des agents sont validés avant persistance
- **CORS** : origins autorisées configurées explicitement (pas de wildcard en prod)
- **TLS** : terminaison TLS obligatoire en prod (Ingress K8s ou reverse proxy)
- **Passwords** : hashés avec bcrypt (cost ≥ 12)
