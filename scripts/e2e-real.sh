#!/usr/bin/env bash
# Spin up the full otel-magnify stack against SQLite and run the real-backend
# Playwright suite end-to-end. Tears down on exit (success or failure).
#
# Usage: ./scripts/e2e-real.sh
# Requires: docker, npx (playwright installed in frontend/)

set -euo pipefail

cd "$(dirname "$0")/.."

# Test credentials — fixed so re-runs on the same DB volume are predictable.
# The volume is wiped by `docker compose down -v` at the end of each run.
export JWT_SECRET="e2e-real-jwt-secret"
export SEED_ADMIN_EMAIL="admin@e2e.local"
export SEED_ADMIN_PASSWORD="initialPass!!!12"

cleanup() {
  echo "--- docker compose down -v ---"
  docker compose -p otel-magnify-e2e down -v >/dev/null 2>&1 || true
}
trap cleanup EXIT

# Wipe any leftover volume from a previous aborted run before starting.
docker compose -p otel-magnify-e2e down -v >/dev/null 2>&1 || true

echo "--- docker compose up (build + detach) ---"
docker compose -p otel-magnify-e2e up -d --build

echo "--- waiting for /api/auth/methods (up to 90s) ---"
for i in $(seq 1 90); do
  if curl -sf http://localhost:8080/api/auth/methods >/dev/null 2>&1; then
    echo "server ready after ${i}s"
    break
  fi
  sleep 1
  if [ "$i" -eq 90 ]; then
    echo "server did not become ready in 90s"
    docker compose -p otel-magnify-e2e logs --tail=100
    exit 1
  fi
done

echo "--- running Playwright real suite ---"
cd frontend
npx playwright test --config=playwright.real.config.ts "$@"
