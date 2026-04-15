# Troubleshooting

> **Status:** This page is a stub. It will grow as user questions arrive.

## Planned content

- Agent connects but never appears in the inventory — check `OPAMP_ADDR`, WebSocket upgrade, reverse-proxy `Connection: Upgrade` headers.
- Push succeeds in the UI but the agent shows `FAILED` — inspect the error message stored in the push history.
- Auto-rollback loops — usually a bad last-known-good config; reset via the API.
- WebSocket disconnects in the UI — check the JWT `?token=` query parameter and its expiry.
- SQLite "database is locked" under load — switch to PostgreSQL.
