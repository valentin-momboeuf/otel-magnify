# REST API

All endpoints return JSON and expect JSON request bodies (where applicable). Authenticated endpoints require the header `Authorization: Bearer <jwt>`.

## Endpoint summary

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `POST` | `/api/auth/login` | No | Log in, returns a JWT. |
| `GET` | `/api/agents` | Yes | List all agents. |
| `GET` | `/api/agents/{id}` | Yes | Get agent details. |
| `GET` | `/api/agents/{id}/configs` | Yes | Agent push history. |
| `POST` | `/api/agents/{id}/config` | Yes | Push a config to the agent. |
| `POST` | `/api/agents/{id}/config/validate` | Yes | Lightweight server-side validation of a config. |
| `GET` | `/api/configs` | Yes | List all configs. |
| `POST` | `/api/configs` | Yes | Create a new config. |
| `GET` | `/api/configs/{id}` | Yes | Fetch a config by ID. |
| `GET` | `/api/alerts` | Yes | List active alerts. |
| `POST` | `/api/alerts/{id}/resolve` | Yes | Resolve an alert. |
| `GET` | `/ws?token={jwt}` | Yes | WebSocket hub (see [WebSocket](websocket.md)). |
| `GET` | `/healthz` | No | Liveness probe. |

## Representative payloads

### `POST /api/auth/login`

Request:

```json
{
  "email": "admin@local",
  "password": "changeme"
}
```

Response:

```json
{
  "token": "eyJhbGciOi...",
  "user": { "id": "...", "email": "admin@local", "role": "admin" }
}
```

### `GET /api/agents`

Response is an array of agent summaries. The exact fields are defined in `backend/pkg/models/agent.go`. Treat it as the source of truth; do not hand-maintain the shape here — link to the file from the rendered doc instead.

### `POST /api/agents/{id}/config`

Request:

```json
{
  "content": "receivers:\n  otlp:\n    protocols:\n      grpc:\n..."
}
```

Response includes the persisted `config_hash` and the initial push status (`PENDING`). Follow-up status updates arrive via the WebSocket.

### `POST /api/agents/{id}/config/validate`

Same request shape as the push endpoint. Response is either `{ "valid": true }` or `{ "valid": false, "errors": [ ... ] }`.

## Error format

Errors follow the shape:

```json
{ "error": "human-readable message" }
```

HTTP status codes follow REST conventions: `400` for bad input, `401` for missing/expired JWT, `403` for insufficient role, `404` for unknown IDs, `500` for server errors.
