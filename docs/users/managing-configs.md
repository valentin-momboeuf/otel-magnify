# Managing configs

otel-magnify stores configurations centrally and pushes them to connected agents over OpAMP. Each push is recorded with its hash, the operator who triggered it, the reported status, and the error message (if any).

## Workflow

1. Open an agent from the **Inventory** page.
2. Edit the YAML in the embedded CodeMirror editor.
3. Click **Validate** — the backend runs a light structural check and blocks the push if errors are found. Errors are listed inline.
4. Click **Push** to send the configuration to the agent.
5. The agent reports a `RemoteConfigStatus` — the UI updates live via WebSocket.

## Validation

The `POST /api/agents/{id}/config/validate` endpoint performs a lightweight YAML sanity check against the agent's reported `AvailableComponents`. It does **not** attempt a full Collector-side validation; the agent is the ultimate authority. If the agent rejects the config after push, otel-magnify records the error message returned by the agent.

## Auto-rollback

When an agent reports a `failed` status, otel-magnify automatically re-pushes the last known-good configuration. The rollback is recorded as a **new** `agent_configs` row (status `pending`, `pushed_by = "auto-rollback"`) — the failed row is left in place for auditing. An `auto_rollback_applied` event is broadcast on the WebSocket.

## Push history

Every push is stored in the `agent_configs` table with:

- Config content hash
- Operator (`pushed_by` — the user's email, or `auto-rollback` for automated recoveries)
- Timestamp
- Status (`pending`, `applied`, or `failed`)
- Error message if the agent rejected the config

The history is visible from the agent detail page.
