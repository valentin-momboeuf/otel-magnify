#!/usr/bin/env bash
# Run the destructive-to-capacity, but isolated, 5,000 collector OpAMP scenario.

set -euo pipefail
umask 077

if [[ "${LOAD_TEST_CONFIRM:-}" != "5000" ]]; then
  echo "set LOAD_TEST_CONFIRM=5000 to run the 5,000 collector load test" >&2
  exit 2
fi

load_project_name=""
load_temporary_directory=""
load_temporary_parent=""
load_secrets_directory=""
load_artifacts_directory=""
load_compose_override=""
load_cookie_jar=""
load_token_file=""
load_token_response_file=""
load_token_id=""
load_api_url=""
load_client_pid=""
load_client_container_id=""
load_client_cidfile=""
load_client_exit_file=""
load_client_summary_file=""
load_client_stderr_file=""
load_compose_started=false
load_runtime_uid=""
load_runtime_gid=""
load_cleanup_failed=false
load_foreground_pid=""
load_foreground_launching=false
load_client_launching=false

load_require_command() {
  local command_name="$1"

  if ! command -v "${command_name}" >/dev/null 2>&1; then
    echo "required command not found: ${command_name}" >&2
    exit 2
  fi
}

load_compose() {
  docker compose \
    --env-file /dev/null \
    --project-name "${load_project_name}" \
    --file docker-compose.yml \
    --file "${load_compose_override}" \
    "$@"
}

load_is_safe_temporary_directory() {
  local path="$1"

  [[ -n "${path}" ]] \
    && [[ -d "${path}" ]] \
    && [[ ! -L "${path}" ]] \
    && [[ "$(dirname "${path}")" == "${load_temporary_parent}" ]] \
    && [[ "$(basename "${path}")" == otel-magnify-load-5000.* ]]
}

load_scan_artifacts() {
  local scan_status

  if [[ ! -s "${load_token_file}" ]] || [[ ! -d "${load_artifacts_directory}" ]]; then
    return 0
  fi

  if grep --recursive --fixed-strings --quiet \
    --file="${load_token_file}" \
    "${load_artifacts_directory}"; then
    return 1
  else
    scan_status=$?
  fi

  if [[ "${scan_status}" -eq 1 ]]; then
    return 0
  fi
  return 2
}

load_require_clean_artifacts() {
  local scan_status

  if load_scan_artifacts; then
    return 0
  else
    scan_status=$?
  fi

  if [[ "${scan_status}" -eq 1 ]]; then
    echo "credential material detected in load-test artifacts" >&2
  else
    echo "load-test artifact credential scan failed" >&2
  fi
  return 1
}

load_collect_logs() {
  if [[ "${load_compose_started}" != true ]]; then
    return 0
  fi

  load_compose logs --no-color >"${load_artifacts_directory}/compose.log" 2>&1
}

load_revoke_token() {
  if [[ -z "${load_token_id}" ]] \
    || [[ ! -s "${load_cookie_jar}" ]] \
    || [[ -z "${load_api_url}" ]] \
    || ! curl --fail --silent --max-time 3 "${load_api_url}/readyz" >/dev/null 2>&1; then
    return 0
  fi

  curl --silent --max-time 10 \
    --cookie "${load_cookie_jar}" \
    --request POST \
    --output /dev/null \
    "${load_api_url}/api/v1/opamp/tokens/${load_token_id}/revoke" \
    >/dev/null 2>&1
}

load_reconcile_token_id() {
  if [[ -n "${load_token_id}" ]] \
    || [[ ! -s "${load_token_response_file}" ]]; then
    return 0
  fi

  load_capture_token_id "${load_token_response_file}" >/dev/null 2>&1 || true
}

load_validate_client_container() {
  local candidate_id
  local inspected_id
  local project_label

  if [[ ! -s "${load_client_cidfile}" ]]; then
    echo "load client cidfile was not created" >&2
    return 1
  fi
  candidate_id="$(<"${load_client_cidfile}")"
  if [[ ! "${candidate_id}" =~ ^[0-9a-f]{64}$ ]]; then
    echo "load client cidfile did not contain a valid container ID" >&2
    return 1
  fi
  if ! inspected_id="$(docker inspect --format '{{.Id}}' "${candidate_id}" 2>/dev/null)" \
    || ! project_label="$(docker inspect --format '{{index .Config.Labels "com.magnify.task9.project"}}' "${candidate_id}" 2>/dev/null)" \
    || [[ "${inspected_id}" != "${candidate_id}" ]] \
    || [[ "${project_label}" != "${load_project_name}" ]]; then
    echo "load client cidfile did not identify this invocation's container" >&2
    return 1
  fi
  load_client_container_id="${candidate_id}"
}

load_collect_client_output() {
  if [[ -z "${load_client_container_id}" ]] \
    || [[ -z "${load_client_summary_file}" ]] \
    || [[ -z "${load_client_stderr_file}" ]]; then
    return 0
  fi

  docker logs "${load_client_container_id}" \
    >"${load_client_summary_file}" 2>"${load_client_stderr_file}"
}

load_stop_client() {
  local client_status=0
  local client_running=""

  if [[ -z "${load_client_container_id}" ]] && ! load_validate_client_container; then
    echo "could not validate the load client container for cleanup" >&2
    load_cleanup_failed=true
  fi
  if [[ -n "${load_client_container_id}" ]]; then
    if ! client_running="$(
      docker inspect --format '{{.State.Running}}' "${load_client_container_id}" 2>/dev/null
    )"; then
      echo "failed to inspect the load client container" >&2
      load_cleanup_failed=true
    elif [[ "${client_running}" == true ]]; then
      if ! docker stop --time 10 "${load_client_container_id}" >/dev/null 2>&1; then
        echo "failed to stop the load client container" >&2
        load_cleanup_failed=true
      fi
    fi
  fi
  if [[ "${load_client_launching}" == true ]] \
    && [[ -z "${load_client_pid}" ]]; then
    set +u
    load_client_pid=$!
    set -u
    load_client_launching=false
  fi
  if [[ -n "${load_client_pid}" ]]; then
    wait "${load_client_pid}" >/dev/null 2>&1 || true
    load_client_pid=""
  fi
  if [[ -s "${load_client_exit_file}" ]]; then
    client_status="$(<"${load_client_exit_file}")"
    if [[ ! "${client_status}" =~ ^[0-9]+$ ]] || ((client_status > 255)); then
      client_status=1
      load_cleanup_failed=true
    fi
  elif [[ -n "${load_client_container_id}" ]]; then
    if ! client_status="$(
      docker inspect --format '{{.State.ExitCode}}' "${load_client_container_id}" 2>/dev/null
    )"; then
      client_status=1
      load_cleanup_failed=true
    fi
  fi
  if [[ -n "${load_client_container_id}" ]]; then
    if ! load_collect_client_output; then
      echo "failed to collect load client diagnostics" >&2
      load_cleanup_failed=true
    fi
    if ! docker rm --force "${load_client_container_id}" >/dev/null 2>&1; then
      echo "failed to remove the load client container" >&2
      load_cleanup_failed=true
    fi
  fi

  return "${client_status}"
}

load_cleanup() {
  trap '' EXIT INT TERM
  local exit_code="$1"
  local scan_status=0
  set +e

  load_stop_client >/dev/null
  load_reconcile_token_id
  load_revoke_token

  if [[ "${load_compose_started}" == true ]]; then
    load_collect_logs
    if [[ $? -ne 0 && "${exit_code}" -eq 0 ]]; then
      echo "failed to collect load-test diagnostics" >&2
      exit_code=1
    fi
  fi

  load_scan_artifacts
  scan_status=$?
  if [[ "${scan_status}" -eq 1 ]]; then
    echo "credential material detected in load-test artifacts" >&2
    if [[ "${exit_code}" -eq 0 ]]; then
      exit_code=1
    fi
  elif [[ "${scan_status}" -eq 2 ]]; then
    echo "load-test artifact credential scan failed" >&2
    if [[ "${exit_code}" -eq 0 ]]; then
      exit_code=1
    fi
  fi

  if [[ "${load_compose_started}" == true ]]; then
    if ! load_compose down --volumes --remove-orphans >/dev/null 2>&1; then
      echo "failed to remove the load-test Compose resources" >&2
      load_cleanup_failed=true
    fi
  fi

  if [[ -n "${load_temporary_directory}" ]]; then
    if load_is_safe_temporary_directory "${load_temporary_directory}"; then
      if ! rm -rf -- "${load_temporary_directory}"; then
        echo "failed to remove the load-test temporary directory" >&2
        load_cleanup_failed=true
      fi
    else
      echo "refusing to remove an unexpected load-test temporary directory" >&2
      if [[ "${exit_code}" -eq 0 ]]; then
        exit_code=1
      fi
    fi
  fi

  if [[ "${exit_code}" -eq 0 && "${load_cleanup_failed}" == true ]]; then
    exit_code=1
  fi

  exit "${exit_code}"
}

load_run_foreground_ignoring_signals() {
  local command_status

  load_foreground_launching=true
  (
    trap '' INT TERM
    "$@"
  ) &
  load_foreground_pid=$!
  load_foreground_launching=false
  if wait "${load_foreground_pid}"; then
    command_status=0
  else
    command_status=$?
  fi
  load_foreground_pid=""
  return "${command_status}"
}

load_handle_signal() {
  trap '' INT TERM
  local signal_status="$1"

  if [[ "${load_foreground_launching}" == true ]] \
    && [[ -z "${load_foreground_pid}" ]]; then
    wait || true
    load_foreground_launching=false
  fi
  if [[ -n "${load_foreground_pid}" ]]; then
    if wait "${load_foreground_pid}"; then
      :
    fi
    load_foreground_pid=""
  fi
  if [[ "${load_client_launching}" == true ]] \
    && [[ -z "${load_client_pid}" ]]; then
    set +u
    load_client_pid=$!
    set -u
    load_client_launching=false
  fi
  exit "${signal_status}"
}

load_wait_for_api() {
  local attempt
  local readiness_body

  for ((attempt = 1; attempt <= 120; attempt++)); do
    if readiness_body="$(curl --fail --silent --show-error "${load_api_url}/readyz" 2>/dev/null)" \
      && [[ "${readiness_body}" == "ready" ]]; then
      return 0
    fi
    sleep 1
  done
  echo "server did not become ready within 120 seconds" >&2
  return 1
}

load_create_token() {
  local create_response="$1"
  local create_status

  create_status="$(
    jq --null-input '{name: "load-test-5000", description: "Ephemeral 5,000 connection load credential", environment: "test"}' \
      | curl --silent --show-error \
        --cookie "${load_cookie_jar}" \
        --header 'Content-Type: application/json' \
        --data-binary @- \
        --output "${create_response}" \
        --write-out '%{http_code}' \
        "${load_api_url}/api/v1/opamp/tokens"
  )"
  chmod 600 "${create_response}"

  if [[ "${create_status}" == "503" ]] \
    && jq -e '.side_effect_status == "unknown"' "${create_response}" >/dev/null 2>&1; then
    if ! load_capture_token_id "${create_response}"; then
      echo "load token reconciliation data is invalid" >&2
    fi
    echo "load token creation outcome is unknown; refusing to continue" >&2
    return 1
  fi
  if [[ "${create_status}" != "201" ]]; then
    echo "load token creation failed" >&2
    return 1
  fi

  if ! load_capture_token_id "${create_response}"; then
    echo "load token response did not satisfy the credential contract" >&2
    return 1
  fi
  if ! jq -e --arg id "${load_token_id}" \
    '.value | type == "string" and test("^ompt_" + $id + "\\.[A-Za-z0-9_-]{43}$")' \
    "${create_response}" >/dev/null; then
    echo "load token response did not satisfy the credential contract" >&2
    return 1
  fi

  jq -er '.value' "${create_response}" >"${load_token_file}"
  chmod 600 "${load_token_file}"
  rm -f -- "${create_response}"
}

load_capture_token_id() {
  local response_file="$1"
  local token_id

  token_id="$(
    jq -er '(.token.id? // .token_id?) | select(type == "string" and test("^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"))' \
      "${response_file}"
  )" || return 1
  load_token_id="${token_id}"
}

load_wait_for_collectors() {
  local ready_file="$1"
  local timeout_seconds="$2"
  local attempt

  for ((attempt = 1; attempt <= timeout_seconds; attempt++)); do
    if [[ -s "${ready_file}" ]]; then
      return 0
    fi
    if [[ -z "${load_client_pid}" ]] || ! kill -0 "${load_client_pid}" >/dev/null 2>&1; then
      return 1
    fi
    sleep 1
  done
  return 1
}

load_collect_runtime_evidence() {
  local docker_stats_file="$1"
  local postgres_activity_file="$2"
  local -a container_ids=()
  local container_id

  while IFS= read -r container_id; do
    if [[ -n "${container_id}" ]]; then
      container_ids+=("${container_id}")
    fi
  done < <(load_compose ps --quiet)
  if [[ -n "${load_client_container_id}" ]] \
    && docker inspect "${load_client_container_id}" >/dev/null 2>&1; then
    container_ids+=("${load_client_container_id}")
  fi
  if [[ "${#container_ids[@]}" -gt 0 ]]; then
    docker stats --no-stream "${container_ids[@]}" >"${docker_stats_file}"
  fi

  load_compose exec --no-TTY postgres \
    psql --username magnify --dbname magnify \
    --no-psqlrc --no-align --tuples-only --field-separator '|' \
    --command "SELECT count(*), 40 FROM pg_stat_activity WHERE datname = current_database() AND pid <> pg_backend_pid();" \
    >"${postgres_activity_file}"
}

load_main() {
  local command_name
  for command_name in awk curl date docker grep id jq openssl; do
    load_require_command "${command_name}"
  done

  local repo_root
  repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
  cd "${repo_root}"

  unset DB_DSN OPAMP_TOKEN_FILE COMPOSE_FILE COMPOSE_ENV_FILES

  load_project_name="otel-magnify-load-5000-$$-${RANDOM}"
  if [[ ! "${load_project_name}" =~ ^otel-magnify-load-5000-[0-9]+-[0-9]+$ ]]; then
    echo "could not construct a bounded Compose project name" >&2
    return 1
  fi
  load_temporary_parent="$(cd "${TMPDIR:-/tmp}" && pwd -P)"
  load_temporary_directory="$(
    mktemp -d "${load_temporary_parent}/otel-magnify-load-5000.XXXXXX"
  )"
  chmod 700 "${load_temporary_directory}"
  if ! load_is_safe_temporary_directory "${load_temporary_directory}"; then
    echo "could not create a bounded load-test temporary directory" >&2
    return 1
  fi
  trap 'load_cleanup "$?"' EXIT
  trap 'load_handle_signal 130' INT
  trap 'load_handle_signal 143' TERM
  load_runtime_uid="$(id -u)"
  load_runtime_gid="$(id -g)"

  load_secrets_directory="${load_temporary_directory}/secrets"
  load_artifacts_directory="${load_temporary_directory}/artifacts"
  mkdir -m 700 "${load_secrets_directory}" "${load_artifacts_directory}"

  local jwt_secret_file="${load_secrets_directory}/jwt-secret"
  local postgres_password_file="${load_secrets_directory}/postgres-password"
  local admin_password_file="${load_secrets_directory}/admin-password"
  load_cookie_jar="${load_secrets_directory}/cookies"
  local login_response="${load_secrets_directory}/login.json"
  load_token_response_file="${load_secrets_directory}/token-create.json"
  load_client_cidfile="${load_secrets_directory}/client.cid"
  load_token_file="${load_secrets_directory}/opamp-token"
  load_compose_override="${load_temporary_directory}/compose.override.yml"

  local ready_file="${load_artifacts_directory}/ready.json"
  load_client_summary_file="${load_artifacts_directory}/summary.json"
  load_client_stderr_file="${load_artifacts_directory}/opamp-load.stderr"
  load_client_exit_file="${load_artifacts_directory}/opamp-load.exit"
  local opamp_errors_file="${load_artifacts_directory}/opamp-errors.txt"
  local docker_stats_file="${load_artifacts_directory}/docker-stats.txt"
  local postgres_activity_file="${load_artifacts_directory}/pg-stat-activity.txt"

  openssl rand -hex 32 >"${jwt_secret_file}"
  openssl rand -hex 24 >"${postgres_password_file}"
  openssl rand -hex 24 >"${admin_password_file}"
  chmod 600 "${jwt_secret_file}" "${postgres_password_file}" "${admin_password_file}"

  export JWT_SECRET
  JWT_SECRET="$(<"${jwt_secret_file}")"
  export POSTGRES_PASSWORD
  POSTGRES_PASSWORD="$(<"${postgres_password_file}")"
  export DB_DSN="postgres://magnify:${POSTGRES_PASSWORD}@postgres:5432/magnify?sslmode=disable"
  export SEED_ADMIN_EMAIL="load-admin@example.invalid"
  export SEED_ADMIN_PASSWORD
  SEED_ADMIN_PASSWORD="$(<"${admin_password_file}")"
  export DB_MAX_OPEN_CONNS="40"

  {
    printf '%s\n' \
      'services:' \
      '  otel-magnify:' \
      '    ports: !override' \
      '      - "127.0.0.1::8080"' \
      '    environment:' \
      '      DB_MAX_OPEN_CONNS: "40"' \
      '  postgres:' \
      '    ports: !override []'
  } >"${load_compose_override}"
  chmod 600 "${load_compose_override}"

  load_compose config --quiet
  load_compose_started=true
  load_compose up --detach --build postgres otel-magnify

  local api_binding
  api_binding="$(load_compose port otel-magnify 8080 | head -n 1)"
  if [[ ! "${api_binding}" =~ ^127\.0\.0\.1:([0-9]+)$ ]]; then
    echo "could not resolve the isolated API port" >&2
    return 1
  fi
  load_api_url="http://127.0.0.1:${BASH_REMATCH[1]}"
  load_wait_for_api

  local postgres_version
  postgres_version="$(
    load_compose exec --no-TTY postgres \
      psql --username magnify --dbname magnify --no-align --tuples-only \
      --command 'SHOW server_version'
  )"
  if [[ ! "${postgres_version}" =~ ^18(\.|$) ]]; then
    echo "PostgreSQL 18 is required" >&2
    return 1
  fi

  jq --null-input \
    --arg email "${SEED_ADMIN_EMAIL}" \
    --arg password "${SEED_ADMIN_PASSWORD}" \
    '{email: $email, password: $password}' \
    | curl --fail --silent --show-error \
      --cookie-jar "${load_cookie_jar}" \
      --header 'Content-Type: application/json' \
      --data-binary @- \
      --output "${login_response}" \
      "${load_api_url}/api/auth/login"
  chmod 600 "${load_cookie_jar}" "${login_response}"
  jq -e '.token | type == "string" and length > 0' "${login_response}" >/dev/null

  load_create_token "${load_token_response_file}"

  local load_test_ramp="${LOAD_TEST_RAMP:-5m}"
  local load_test_hold="${LOAD_TEST_HOLD:-10m}"
  local ready_timeout_seconds="${LOAD_TEST_READY_TIMEOUT_SECONDS:-900}"
  if [[ ! "${ready_timeout_seconds}" =~ ^[1-9][0-9]*$ ]]; then
    echo "LOAD_TEST_READY_TIMEOUT_SECONDS must be a positive integer" >&2
    return 2
  fi

  load_run_foreground_ignoring_signals \
    docker create \
    --cidfile "${load_client_cidfile}" \
    --label "com.magnify.task9.project=${load_project_name}" \
    --user "${load_runtime_uid}:${load_runtime_gid}" \
    --network "${load_project_name}_default" \
    --volume "${repo_root}:/app:ro" \
    --volume "${load_token_file}:/run/secrets/opamp-token:ro" \
    --volume "${load_artifacts_directory}:/artifacts" \
    --env HOME=/tmp/home \
    --env GOCACHE=/tmp/go-build \
    --env GOMODCACHE=/tmp/go-mod \
    --tmpfs /tmp:rw,exec,mode=1777 \
    --workdir /app \
    golang:1.25.12 \
    go run ./cmd/opamp-load \
    --endpoint "ws://otel-magnify:4320/v1/opamp" \
    --token-file /run/secrets/opamp-token \
    --allow-insecure-transport \
    --collectors 5000 \
    --ramp "${load_test_ramp}" \
    --hold "${load_test_hold}" \
    --ready-file /artifacts/ready.json \
    >/dev/null

  if ! load_validate_client_container; then
    return 1
  fi

  local client_started_at
  client_started_at="$(date +%s)"
  load_run_foreground_ignoring_signals \
    docker start "${load_client_container_id}" >/dev/null

  load_client_launching=true
  docker wait "${load_client_container_id}" >"${load_client_exit_file}" &
  load_client_pid="$!"
  load_client_launching=false

  if ! load_wait_for_collectors "${ready_file}" "${ready_timeout_seconds}"; then
    echo "opamp-load did not reach the connection hold phase" >&2
    return 1
  fi

  if ! jq -e \
    '.attempted == 5000 and .connected == 5000 and .failed == 0 and .cancelled == 0 and .disconnected == 0 and .stop_failed == 0 and .interrupted == false' \
    "${ready_file}" >/dev/null; then
    echo "collectors did not all reach the hold phase successfully" >&2
    return 1
  fi
  if ! kill -0 "${load_client_pid}" >/dev/null 2>&1; then
    echo "opamp-load exited before hold-phase evidence could be captured" >&2
    return 1
  fi

  local collectors_ready_at
  collectors_ready_at="$(date +%s)"
  local establishment_seconds=$((collectors_ready_at - client_started_at))

  load_collect_runtime_evidence "${docker_stats_file}" "${postgres_activity_file}"
  load_collect_logs
  load_require_clean_artifacts

  if ! awk -F '|' \
    'NF == 2 && $1 ~ /^[0-9]+$/ && $2 == "40" && $1 <= $2 { valid = 1 } END { exit !valid }' \
    "${postgres_activity_file}"; then
    echo "PostgreSQL connections exceeded the configured maximum of 40" >&2
    return 1
  fi

  if grep --extended-regexp --ignore-case "error|failed|panic" \
    "${load_artifacts_directory}/compose.log" >"${opamp_errors_file}"; then
    echo "application errors were recorded while collectors were held" >&2
    return 1
  else
    local grep_status=$?
    if [[ "${grep_status}" -ne 1 ]]; then
      echo "failed to inspect application logs" >&2
      return 1
    fi
  fi

  local client_status
  if ! wait "${load_client_pid}"; then
    echo "failed to wait for the load client container" >&2
    return 1
  fi
  load_client_pid=""
  if [[ ! -s "${load_client_exit_file}" ]]; then
    echo "load client exit status was not recorded" >&2
    return 1
  fi
  client_status="$(<"${load_client_exit_file}")"
  if [[ ! "${client_status}" =~ ^[0-9]+$ ]] || ((client_status > 255)); then
    echo "load client exit status was invalid" >&2
    return 1
  fi
  if ! load_collect_client_output; then
    echo "failed to collect load client output" >&2
    return 1
  fi

  if ! jq -e \
    '.attempted == 5000 and .connected == 5000 and .failed == 0 and .cancelled == 0 and .disconnected == 5000 and .stop_failed == 0 and .interrupted == false' \
    "${load_client_summary_file}" >/dev/null; then
    echo "load test summary did not meet the 5,000 connected collector target" >&2
    return 1
  fi
  if [[ "${client_status}" -ne 0 ]]; then
    echo "opamp-load exited unsuccessfully" >&2
    return "${client_status}"
  fi

  load_collect_logs
  load_require_clean_artifacts

  echo "5,000 collector load test completed"
  echo "connection_establishment_seconds=${establishment_seconds}"
  echo "postgres_version=${postgres_version}"
}

load_main "$@"
