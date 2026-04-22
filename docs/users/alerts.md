# Alerts

> **Status:** This page is a stub. Help wanted — see [CONTRIBUTING.md](https://github.com/magnify-labs/otel-magnify/blob/main/CONTRIBUTING.md).

## Planned content

- Alert engine overview — 30-second evaluation loop in `backend/internal/alerts/`.
- Current rules:
    - `workload_down` — every instance of the workload has disconnected and the grace window has elapsed (supersedes the earlier `agent_down` rule).
    - Workload `service.version` below `MIN_AGENT_VERSION` (if set).
- Webhook notifier — `WEBHOOK_URL` payload format.
- Resolving alerts from the UI.
- Planned rules: config drift, repeated push failures.
