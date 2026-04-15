# OpAMP endpoint

> **Status:** Stub — to be expanded.

## Planned content

- Default endpoint: `ws://<host>:4320/v1/opamp`. Configurable via `OPAMP_ADDR`.
- Capabilities currently honored by otel-magnify:
    - `ReportsEffectiveConfig`
    - `ReportsRemoteConfig` / `AcceptsRemoteConfig`
    - `ReportsHealth`
    - `ReportsAvailableComponents`
- `AvailableComponents` capture and how it is used to validate pushed configs (see commit `5bf11da`).
- Mapping from OpAMP `AgentDescription` fields to the otel-magnify agent model.
- Agent type detection via `service.name` (collectors vs SDK agents).
- TLS: currently served plain-WS; terminate TLS at a reverse proxy for production.
