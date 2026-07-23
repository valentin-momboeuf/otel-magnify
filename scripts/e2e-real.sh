#!/usr/bin/env bash
# Spin up the full otel-magnify stack against PostgreSQL and run the
# real-backend Playwright suite end-to-end. Tears down on exit (success or failure).
#
# Usage: ./scripts/e2e-real.sh
# Requires: docker, npx (playwright installed in frontend/)

set -euo pipefail
umask 077

cd "$(dirname "$0")/.."

# Test credentials — fixed so re-runs on the same DB volume are predictable.
# The volume is wiped by `docker compose down -v` at the end of each run.
: "${JWT_SECRET:=e2e-real-jwt-secret-test-only-32b}"
if [ "${#JWT_SECRET}" -lt 32 ]; then
  echo "JWT_SECRET must be at least 32 characters for the real E2E suite" >&2
  exit 1
fi
export JWT_SECRET
export SEED_ADMIN_EMAIL="admin@e2e.local"
export SEED_ADMIN_PASSWORD="initialPass!!!12"
export POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-e2e-real-postgres-password}"
export DB_DSN="${DB_DSN:-postgres://magnify:${POSTGRES_PASSWORD}@postgres:5432/magnify?sslmode=disable}"

artifact_parent=""
playwright_log=""

# ShellCheck cannot infer this EXIT-trap invocation once the test status is preserved explicitly.
# shellcheck disable=SC2329
cleanup() {
  original_status="$?"
  final_status="$original_status"
  set +e

  artifact_scan_clean="true"
  if [ -d "$artifact_parent" ]; then
    if rg --hidden --no-ignore --quiet --text \
      'ompt_[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\.[A-Za-z0-9_-]{43}' \
      "$artifact_parent"; then
      echo "credential material detected in Playwright artifacts" >&2
      final_status=1
      artifact_scan_clean="false"
    elif [ "$?" -ne 1 ]; then
      echo "Playwright artifact scan failed" >&2
      final_status=1
      artifact_scan_clean="false"
    fi
  elif [ -n "$artifact_parent" ]; then
    echo "Playwright artifact scan failed" >&2
    final_status=1
    artifact_scan_clean="false"
  fi

  if [ "$artifact_scan_clean" = "true" ] && [ -f "$playwright_log" ]; then
    cat "$playwright_log"
  fi

  echo "--- docker compose down -v ---"
  docker compose -p otel-magnify-e2e down -v >/dev/null 2>&1 || true

  if [ -n "$artifact_parent" ]; then
    case "$artifact_parent" in
      "${TMPDIR:-/tmp}"/otel-magnify-playwright.*)
        if [ -d "$artifact_parent" ] && [ ! -L "$artifact_parent" ]; then
          if ! rm -rf -- "$artifact_parent"; then
            echo "refusing to remove an invalid Playwright artifact directory" >&2
            final_status=1
          fi
        else
          echo "refusing to remove an invalid Playwright artifact directory" >&2
          final_status=1
        fi
        ;;
      *)
        echo "refusing to remove an invalid Playwright artifact directory" >&2
        final_status=1
        ;;
    esac
  fi

  trap - EXIT
  exit "$final_status"
}
trap cleanup EXIT

artifact_parent="$(mktemp -d "${TMPDIR:-/tmp}/otel-magnify-playwright.XXXXXX")"
chmod 0700 "$artifact_parent"
playwright_log="$artifact_parent/stdout-stderr.log"
export PLAYWRIGHT_OUTPUT_DIR="$artifact_parent/output"

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

echo "--- smoke: config-versioning routing + JSON shape ---"
# Catch routing/JSON regressions on /configs/{hash}/{label,rollback,GET} before
# we burn time on Playwright. These curl checks need only a working API and
# the seeded admin — no agent connection required.
TOKEN="$(curl -fsS -X POST http://localhost:8080/api/auth/login \
  -H 'Content-Type: application/json' \
  -d "{\"email\":\"${SEED_ADMIN_EMAIL}\",\"password\":\"${SEED_ADMIN_PASSWORD}\"}" \
  | grep -o '"token":"[^"]*"' | cut -d'"' -f4)"
if [ -z "$TOKEN" ]; then
  echo "smoke: failed to login as ${SEED_ADMIN_EMAIL}"
  exit 1
fi

# 401 paths (no header) — every versioning endpoint must reject anonymous calls.
for endpoint in \
    "POST /api/workloads/foo/configs/bar/label" \
    "GET  /api/workloads/foo/configs/bar" \
    "POST /api/workloads/foo/configs/bar/rollback"; do
  method="${endpoint%% *}"
  path="${endpoint##* }"
  code="$(curl -s -o /dev/null -w '%{http_code}' -X "$method" "http://localhost:8080${path}")"
  if [ "$code" != "401" ]; then
    echo "smoke: expected 401 on ${method} ${path}, got ${code}"
    exit 1
  fi
done

# Authenticated 404 path on an unknown hash — proves routing + handler return
# the JSON {"error":...} shape the frontend renders.
not_found_body="$(curl -sS -X GET http://localhost:8080/api/workloads/ghost/configs/ghost \
  -H "Authorization: Bearer ${TOKEN}" -o /dev/null -w '%{http_code}')"
if [ "$not_found_body" != "404" ]; then
  echo "smoke: expected 404 on GET /api/workloads/ghost/configs/ghost, got ${not_found_body}"
  exit 1
fi

echo "smoke: config-versioning OK"

echo "--- running Playwright real suite ---"
cd frontend
set +e
npx playwright test --config=playwright.real.config.ts "$@" >"$playwright_log" 2>&1
playwright_status="$?"
set -e
exit "$playwright_status"
