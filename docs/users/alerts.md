# Alerts

> **Status:** This page is a stub. Help wanted — see [CONTRIBUTING.md](https://github.com/valentin-momboeuf/otel-magnify/blob/main/CONTRIBUTING.md).

## Planned content

- Alert engine overview — 30-second evaluation loop in `backend/internal/alerts/`.
- Current rules:
    - Agent offline (no OpAMP heartbeat for N seconds).
    - Agent version below `MIN_AGENT_VERSION` (if set).
- Webhook notifier — `WEBHOOK_URL` payload format.
- Resolving alerts from the UI.
- Planned rules: config drift, repeated push failures.
