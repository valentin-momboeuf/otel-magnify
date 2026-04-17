# OpAMP endpoint

> **Status:** Stub — to be expanded.

## Planned content

- Default endpoint: `ws://<host>:4320/v1/opamp`. Configurable via `OPAMP_ADDR`.
- Capabilities currently honored by otel-magnify:
    - `ReportsEffectiveConfig`
    - `ReportsRemoteConfig` / `AcceptsRemoteConfig` — `AcceptsRemoteConfig` is persisted on the `agents` table as `accepts_remote_config` and exposed in the Agent JSON; the frontend uses it to gate Edit affordances and the API returns `409 Conflict` (`code: remote_config_unsupported`) on `POST /api/agents/{id}/config` when it's false.
    - `ReportsHealth`
    - `ReportsAvailableComponents`
- `AvailableComponents` capture and how it is used to validate pushed configs (see commit `5bf11da`).
- Mapping from OpAMP `AgentDescription` fields to the otel-magnify agent model.
- Agent type detection via `service.name` (collectors vs SDK agents).
- TLS: currently served plain-WS; terminate TLS at a reverse proxy for production.
