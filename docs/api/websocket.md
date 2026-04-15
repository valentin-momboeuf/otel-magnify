# WebSocket

The WebSocket hub at `/ws` streams real-time events to browsers and backend integrations. Auth is via a query-string JWT: `/ws?token=<jwt>`. Browsers cannot set custom headers on WebSocket handshakes, so the token-in-query pattern is required.

## Message envelope

Every message is a JSON object with a `type` discriminator and a `payload`:

```json
{
  "type": "agent_status_changed",
  "payload": { /* type-specific */ }
}
```

## Event types

| Type | When it fires | Payload summary |
|------|---------------|-----------------|
| `agent_registered` | A new agent connects for the first time. | Agent summary. |
| `agent_status_changed` | An agent transitions between states (e.g. healthy → offline). | `{ agent_id, status, last_seen }`. |
| `agent_config_status` | The agent reports a `RemoteConfigStatus` for a pushed config. | `{ agent_id, config_hash, status, error_message }`. |
| `auto_rollback_applied` | The server auto-pushed the last-known-good config after a failure. | `{ agent_id, from_hash, to_hash }`. |
| `alert_fired` | The alert engine raised a new alert. | Alert object. |
| `alert_resolved` | An alert was resolved manually or by the engine. | `{ alert_id }`. |

For authoritative payload shapes, consult `backend/internal/api/ws_events.go` (or the matching file in the current revision).

## Reconnect behavior

The browser client reconnects with exponential backoff capped at a few seconds. Integrations should do the same and treat any missed events as eventually reconciled by a fresh `GET /api/agents` call.
