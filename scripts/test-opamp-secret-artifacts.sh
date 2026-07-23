#!/usr/bin/env bash

set -euo pipefail
umask 077

cd "$(dirname "$0")/.."

artifact_parent=""

artifact_cleanup() {
  cleanup_status="$?"
  set +e

  if [ -n "$artifact_parent" ]; then
    case "$artifact_parent" in
      "${TMPDIR:-/tmp}"/otel-magnify-secret-probe.*)
        if [ -d "$artifact_parent" ] && [ ! -L "$artifact_parent" ]; then
          if ! rm -rf -- "$artifact_parent"; then
            cleanup_status=1
          fi
        else
          cleanup_status=1
        fi
        ;;
      *)
        cleanup_status=1
        ;;
    esac
  fi

  trap - EXIT
  exit "$cleanup_status"
}
trap artifact_cleanup EXIT

artifact_parent="$(mktemp -d "${TMPDIR:-/tmp}/otel-magnify-secret-probe.XXXXXX")"
chmod 0700 "$artifact_parent"

set +e
OPAMP_SECRET_ARTIFACT_PROBE=1 \
PLAYWRIGHT_HTML_OPEN=never \
PLAYWRIGHT_HTML_OUTPUT_DIR="$artifact_parent/report" \
  npx --prefix frontend playwright test \
    --config=frontend/playwright.config.ts \
    frontend/tests/e2e/opamp-token-secret-artifact-failure.spec.ts \
    --project=chromium \
    --workers=1 \
    --retries=0 \
    --output="$artifact_parent/output" \
    --reporter=list,html \
    >"$artifact_parent/stdout-stderr.log" 2>&1
probe_status="$?"
set -e

if [ "$probe_status" -eq 0 ]; then
  echo "controlled Playwright artifact probe did not fail as expected" >&2
  exit 1
fi

if rg --hidden --no-ignore --quiet --text \
  'ompt_[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\.[A-Za-z0-9_-]{43}' \
  "$artifact_parent"; then
  echo "credential material detected in controlled Playwright artifacts" >&2
  exit 1
elif [ "$?" -ne 1 ]; then
  echo "controlled Playwright artifact scan failed" >&2
  exit 1
fi

if ! rg --fixed-strings --quiet -- 'Error: controlled artifact probe failure' \
  "$artifact_parent/stdout-stderr.log"; then
  echo "controlled Playwright artifact probe did not reach its expected failure" >&2
  exit 1
fi
