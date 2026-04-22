# Testing

> **Status:** Stub — to be expanded.

## Planned content

- Go tests: `go test ./...`. In-memory SQLite used for store tests.
- Frontend type check: `cd frontend && npx tsc --noEmit`.
- Playwright E2E: `cd frontend && npm run test:e2e`. Scaffolded in commit `80b33a0`.
- SDK agent simulator: `cmd/sdkagent/` for exercising the OpAMP pipeline without a real Collector.
- Docker Compose loop for integration tests against real PostgreSQL.
