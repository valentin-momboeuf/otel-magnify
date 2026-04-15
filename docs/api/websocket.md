# WebSocket

The WebSocket hub at `/ws` streams real-time events to browsers and backend integrations. Auth is via a query-string JWT: `/ws?token=<jwt>`. Browsers cannot set custom headers on WebSocket handshakes, so the token-in-query pattern is required.

## Message envelope

Every message is a JSON object with a `type` discriminator. Additional fields are flat on the root — there is no `payload` wrapper:

```json
{
  "type": "agent_update",
  "agent": { /* models.Agent */ }
}
```

## Event types

| Type | When it fires | Other fields |
|------|---------------|--------------|
| `agent_update` | An agent is registered or mutated (status, labels, remote config status snapshot). | `agent` — full `models.Agent`. |
| `alert_update` | An alert transitions (fired, acknowledged, resolved). | `alert` — full `models.Alert`. |
| `agent_config_status` | The agent reports a `RemoteConfigStatus` for a pushed config. | `agent_id`, `status` — `{ status, config_hash, error_message, updated_at }`. |
| `auto_rollback_applied` | The server auto-pushed the last-known-good config after a failure. | `agent_id`, `from_hash`, `to_hash`, `reason`. |

For authoritative payload shapes, consult `backend/internal/api/wshub.go` (or the matching file in the current revision).

## Reconnect behavior

The browser client reconnects with exponential backoff capped at a few seconds. Integrations should do the same and treat any missed events as eventually reconciled by a fresh `GET /api/agents` call.
