# Environment variables

Exhaustive reference. See [Configuration](../users/configuration.md) for a user-oriented walkthrough.

| Variable | Required | Default | Scope | Description |
|----------|----------|---------|-------|-------------|
| `JWT_SECRET` | Yes | — | Auth | HS256 signing key for JWT tokens. |
| `LISTEN_ADDR` | No | `:8080` | API | HTTP listen address for the API and embedded frontend. |
| `OPAMP_ADDR` | No | `:4320` | OpAMP | WebSocket listen address for the OpAMP server. |
| `CORS_ORIGINS` | No | `http://localhost:5173` | API | Comma-separated list of allowed origins. |
| `DB_DRIVER` | No | `sqlite` | Store | `sqlite` (default) or `pgx` for PostgreSQL. |
| `DB_DSN` | No | `otel-magnify.db` | Store | SQLite file path or PostgreSQL DSN. |
| `SEED_ADMIN_EMAIL` | No | — | Bootstrap | If set with `SEED_ADMIN_PASSWORD`, creates a first admin user on startup. |
| `SEED_ADMIN_PASSWORD` | No | — | Bootstrap | Password for the seeded admin. |
| `WEBHOOK_URL` | No | — | Alerts | HTTP endpoint called when a new alert fires. |
| `MIN_AGENT_VERSION` | No | — | Alerts | Minimum agent version; agents below are flagged. |
