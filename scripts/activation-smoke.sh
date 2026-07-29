#!/usr/bin/env bash

set -euo pipefail
umask 077

readonly ACTIVATION_API_URL="${ACTIVATION_API_URL:-http://127.0.0.1:8080}"
readonly ACTIVATION_TIMEOUT_SECONDS="${ACTIVATION_TIMEOUT_SECONDS:-900}"
readonly ACTIVATION_WORKLOAD_NAME="otelcol-activation-demo"

activation_project_name=""
activation_temporary_directory=""
activation_temporary_parent=""
activation_artifacts_directory=""
activation_cookie_jar=""
activation_token_file=""
activation_token_response_file=""
activation_token_id=""
activation_compose_started=false
activation_cleanup_failed=false
activation_foreground_pid=""
activation_foreground_launching=false

activation_require_command() {
  local command_name="$1"

  if ! command -v "${command_name}" >/dev/null 2>&1; then
    echo "required command not found: ${command_name}" >&2
    exit 1
  fi
}

activation_compose() {
  docker compose \
    --env-file /dev/null \
    --file docker-compose.yml \
    --project-name "${activation_project_name}" \
    --profile activation \
    "$@"
}

activation_is_safe_temporary_directory() {
  local path="$1"

  [[ -n "${path}" ]] \
    && [[ -d "${path}" ]] \
    && [[ ! -L "${path}" ]] \
    && [[ "$(dirname "${path}")" == "${activation_temporary_parent}" ]] \
    && [[ "$(basename "${path}")" == otel-magnify-activation.* ]]
}

activation_wait_for_readiness() {
  local deadline="$1"
  local readiness_body

  until readiness_body="$(curl --fail --silent --show-error "${ACTIVATION_API_URL}/readyz" 2>/dev/null)" \
    && [[ "${readiness_body}" == "ready" ]]; do
    if ((SECONDS >= deadline)); then
      echo "timed out waiting for the exact API readiness response" >&2
      return 1
    fi
    sleep 2
  done
}

activation_wait_for_workload() {
  local cookie_jar="$1"
  local response_file="$2"
  local deadline="$3"

  until curl --fail --silent --show-error \
    --cookie "${cookie_jar}" \
    --output "${response_file}" \
    "${ACTIVATION_API_URL}/api/workloads" \
    && jq -e --arg name "${ACTIVATION_WORKLOAD_NAME}" \
      '(. // [])[] | select(.display_name == $name and .type == "collector" and .status == "connected" and .accepts_remote_config == true)' \
      "${response_file}" >/dev/null; do
    if ((SECONDS >= deadline)); then
      echo "timed out waiting for the remote-config workload" >&2
      return 1
    fi
    sleep 2
  done
}

activation_wait_for_applied_config() {
  local cookie_jar="$1"
  local workload_id="$2"
  local response_file="$3"
  local deadline="$4"

  until curl --fail --silent --show-error \
    --cookie "${cookie_jar}" \
    --output "${response_file}" \
    "${ACTIVATION_API_URL}/api/workloads/${workload_id}/configs" \
    && jq -e '.[0].status == "applied"' "${response_file}" >/dev/null; do
    if ((SECONDS >= deadline)); then
      echo "timed out waiting for the governed config to be applied" >&2
      return 1
    fi
    sleep 2
  done
}

activation_revoke_token() {
  if [[ -z "${activation_token_id}" ]] \
    || [[ ! -s "${activation_cookie_jar}" ]] \
    || ! curl --fail --silent --max-time 3 "${ACTIVATION_API_URL}/readyz" >/dev/null 2>&1; then
    return 0
  fi

  curl --silent --max-time 10 \
    --cookie "${activation_cookie_jar}" \
    --request POST \
    --output /dev/null \
    "${ACTIVATION_API_URL}/api/v1/opamp/tokens/${activation_token_id}/revoke" \
    >/dev/null 2>&1
}

activation_reconcile_token_id() {
  if [[ -n "${activation_token_id}" ]] \
    || [[ ! -s "${activation_token_response_file}" ]]; then
    return 0
  fi

  activation_capture_token_id "${activation_token_response_file}" >/dev/null 2>&1 || true
}

activation_scan_artifacts() {
  if [[ ! -s "${activation_token_file}" ]] || [[ ! -d "${activation_artifacts_directory}" ]]; then
    return 0
  fi

  grep --recursive --fixed-strings --quiet \
    --file="${activation_token_file}" \
    "${activation_artifacts_directory}"
  local scan_status=$?
  case "${scan_status}" in
    0)
      return 1
      ;;
    1)
      return 0
      ;;
    *)
      return 2
      ;;
  esac
}

activation_cleanup() {
  trap '' EXIT INT TERM
  local exit_code="$1"
  local scan_status=0
  set +e

  if [[ "${activation_compose_started}" == true ]]; then
    activation_reconcile_token_id
    activation_revoke_token
    activation_compose logs --no-color >"${activation_artifacts_directory}/compose.log" 2>&1
    if [[ $? -ne 0 && "${exit_code}" -eq 0 ]]; then
      echo "failed to collect activation diagnostics" >&2
      exit_code=1
    fi

    activation_scan_artifacts
    scan_status=$?
    if [[ "${scan_status}" -eq 1 ]]; then
      echo "credential material detected in activation artifacts" >&2
      if [[ "${exit_code}" -eq 0 ]]; then
        exit_code=1
      fi
    elif [[ "${scan_status}" -eq 2 ]]; then
      echo "activation artifact credential scan failed" >&2
      if [[ "${exit_code}" -eq 0 ]]; then
        exit_code=1
      fi
    fi

    if ! activation_compose down --volumes --remove-orphans >/dev/null 2>&1; then
      echo "failed to remove the activation Compose resources" >&2
      activation_cleanup_failed=true
    fi
  fi

  if [[ -n "${activation_temporary_directory}" ]]; then
    if activation_is_safe_temporary_directory "${activation_temporary_directory}"; then
      if ! rm -rf -- "${activation_temporary_directory}"; then
        echo "failed to remove the activation temporary directory" >&2
        activation_cleanup_failed=true
      fi
    else
      echo "refusing to remove an unexpected activation temporary directory" >&2
      if [[ "${exit_code}" -eq 0 ]]; then
        exit_code=1
      fi
    fi
  fi

  if [[ "${exit_code}" -eq 0 && "${activation_cleanup_failed}" == true ]]; then
    exit_code=1
  fi

  exit "${exit_code}"
}

activation_run_foreground() {
  local command_status

  activation_foreground_launching=true
  "$@" &
  activation_foreground_pid=$!
  activation_foreground_launching=false
  if wait "${activation_foreground_pid}"; then
    command_status=0
  else
    command_status=$?
  fi
  activation_foreground_pid=""
  return "${command_status}"
}

activation_handle_signal() {
  trap '' INT TERM
  local signal_status="$1"

  if [[ "${activation_foreground_launching}" == true ]] \
    && [[ -z "${activation_foreground_pid}" ]]; then
    wait || true
    activation_foreground_launching=false
  fi
  if [[ -n "${activation_foreground_pid}" ]]; then
    if wait "${activation_foreground_pid}"; then
      :
    fi
    activation_foreground_pid=""
  fi
  exit "${signal_status}"
}

activation_create_token() {
  local create_response="$1"
  local token_file="$2"
  local create_status

  create_status="$(
    jq --null-input '{name: "activation-smoke", description: "Ephemeral activation smoke credential", environment: "test"}' \
      | curl --silent --show-error \
        --cookie "${activation_cookie_jar}" \
        --header 'Content-Type: application/json' \
        --data-binary @- \
        --output "${create_response}" \
        --write-out '%{http_code}' \
        "${ACTIVATION_API_URL}/api/v1/opamp/tokens"
  )"
  chmod 600 "${create_response}"

  if [[ "${create_status}" == "503" ]] \
    && jq -e '.side_effect_status == "unknown"' "${create_response}" >/dev/null 2>&1; then
    if ! activation_capture_token_id "${create_response}"; then
      echo "activation token reconciliation data is invalid" >&2
    fi
    echo "activation token creation outcome is unknown; refusing to continue" >&2
    return 1
  fi
  if [[ "${create_status}" != "201" ]]; then
    echo "activation token creation failed" >&2
    return 1
  fi

  if ! activation_capture_token_id "${create_response}"; then
    echo "activation token response did not satisfy the credential contract" >&2
    return 1
  fi
  if ! jq -e --arg id "${activation_token_id}" \
    '.value | type == "string" and test("^ompt_" + $id + "\\.[A-Za-z0-9_-]{43}$")' \
    "${create_response}" >/dev/null; then
    echo "activation token response did not satisfy the credential contract" >&2
    return 1
  fi

  jq -er '.value' "${create_response}" >"${token_file}"
  chmod 600 "${token_file}"
  rm -f -- "${create_response}"
}

activation_capture_token_id() {
  local response_file="$1"
  local token_id

  token_id="$(
    jq -er '(.token.id? // .token_id?) | select(type == "string" and test("^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"))' \
      "${response_file}"
  )" || return 1
  activation_token_id="${token_id}"
}

activation_main() {
  local command_name
  for command_name in curl docker id jq openssl; do
    activation_require_command "${command_name}"
  done
  if [[ "$(id -u)" == "0" ]]; then
    echo "activation smoke must not run as root" >&2
    return 1
  fi

  local repo_root
  repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
  cd "${repo_root}"

  unset DB_DSN OPAMP_TOKEN_FILE COMPOSE_FILE COMPOSE_ENV_FILES

  activation_project_name="otel-magnify-activation-$$-${RANDOM}"
  if [[ ! "${activation_project_name}" =~ ^otel-magnify-activation-[0-9]+-[0-9]+$ ]]; then
    echo "could not construct a bounded Compose project name" >&2
    return 1
  fi

  activation_temporary_parent="$(cd "${TMPDIR:-/tmp}" && pwd -P)"
  activation_temporary_directory="$(
    mktemp -d "${activation_temporary_parent}/otel-magnify-activation.XXXXXX"
  )"
  chmod 700 "${activation_temporary_directory}"
  if ! activation_is_safe_temporary_directory "${activation_temporary_directory}"; then
    echo "could not create a bounded activation temporary directory" >&2
    return 1
  fi
  trap 'activation_cleanup "$?"' EXIT
  trap 'activation_handle_signal 130' INT
  trap 'activation_handle_signal 143' TERM

  local secrets_directory="${activation_temporary_directory}/secrets"
  activation_artifacts_directory="${activation_temporary_directory}/artifacts"
  mkdir -m 700 "${secrets_directory}" "${activation_artifacts_directory}"

  local jwt_secret_file="${secrets_directory}/jwt-secret"
  local postgres_password_file="${secrets_directory}/postgres-password"
  local admin_password_file="${secrets_directory}/admin-password"
  activation_cookie_jar="${secrets_directory}/cookies"
  local login_response="${secrets_directory}/login.json"
  activation_token_response_file="${secrets_directory}/token-create.json"
  activation_token_file="${secrets_directory}/opamp-token"
  local response_file="${activation_artifacts_directory}/response.json"
  local anonymous_docker_config="${activation_temporary_directory}/docker-config"
  mkdir -m 700 "${anonymous_docker_config}"

  openssl rand -hex 32 >"${jwt_secret_file}"
  openssl rand -hex 24 >"${postgres_password_file}"
  openssl rand -hex 24 >"${admin_password_file}"
  chmod 600 "${jwt_secret_file}" "${postgres_password_file}" "${admin_password_file}"

  export JWT_SECRET
  JWT_SECRET="$(<"${jwt_secret_file}")"
  export POSTGRES_PASSWORD
  POSTGRES_PASSWORD="$(<"${postgres_password_file}")"
  export DB_DSN="postgres://magnify:${POSTGRES_PASSWORD}@postgres:5432/magnify?sslmode=disable"
  export SEED_ADMIN_EMAIL="activation-admin@example.invalid"
  export SEED_ADMIN_PASSWORD
  SEED_ADMIN_PASSWORD="$(<"${admin_password_file}")"

  local started_at
  started_at="$(date +%s)"

  if ! activation_compose config --services | grep -Fxq activation-agent; then
    echo "the Compose activation profile must define activation-agent" >&2
    return 1
  fi

  if [[ -n "${OTEL_MAGNIFY_IMAGE:-}" ]]; then
    DOCKER_CONFIG="${anonymous_docker_config}" docker pull "${OTEL_MAGNIFY_IMAGE}" >/dev/null
    activation_compose build activation-agent
    activation_compose_started=true
    activation_run_foreground activation_compose up --detach --no-build postgres otel-magnify
  else
    activation_compose_started=true
    activation_run_foreground activation_compose up --detach --build postgres otel-magnify
  fi

  local deadline=$((SECONDS + ACTIVATION_TIMEOUT_SECONDS))
  activation_wait_for_readiness "${deadline}"

  local postgres_version
  postgres_version="$(
    activation_compose exec --no-TTY postgres \
      psql --username magnify --dbname magnify --no-align --tuples-only \
      --command 'SHOW server_version'
  )"
  local postgres_major
  if [[ "${postgres_version}" =~ ^([0-9]+)(\.|$) ]]; then
    postgres_major="${BASH_REMATCH[1]}"
  else
    echo "could not parse PostgreSQL server version" >&2
    return 1
  fi
  if [[ "${postgres_major}" != "18" ]]; then
    echo "PostgreSQL 18 is required" >&2
    return 1
  fi

  curl --fail --silent --show-error \
    --output "${response_file}" \
    "${ACTIVATION_API_URL}/api/features"
  jq -e '.features == {
    "config_safety.approvals": true,
    "config_safety.policy_preview": true
  }' "${response_file}" >/dev/null

  jq --null-input \
    --arg email "${SEED_ADMIN_EMAIL}" \
    --arg password "${SEED_ADMIN_PASSWORD}" \
    '{email: $email, password: $password}' \
    | curl --fail --silent --show-error \
      --cookie-jar "${activation_cookie_jar}" \
      --header 'Content-Type: application/json' \
      --data-binary @- \
      --output "${login_response}" \
      "${ACTIVATION_API_URL}/api/auth/login"
  chmod 600 "${activation_cookie_jar}" "${login_response}"
  jq -e '.token | type == "string" and length > 0' "${login_response}" >/dev/null

  activation_create_token "${activation_token_response_file}" "${activation_token_file}"

  export OPAMP_TOKEN_FILE="${activation_token_file}"
  export OPAMP_RUNTIME_UID
  OPAMP_RUNTIME_UID="$(id -u)"
  export OPAMP_RUNTIME_GID
  OPAMP_RUNTIME_GID="$(id -g)"

  if [[ -n "${OTEL_MAGNIFY_IMAGE:-}" ]]; then
    activation_run_foreground activation_compose up --detach --no-build activation-agent
  else
    activation_run_foreground activation_compose up --detach --build activation-agent
  fi

  activation_wait_for_workload "${activation_cookie_jar}" "${response_file}" "${deadline}"
  local workload_id
  workload_id="$(
    jq -er --arg name "${ACTIVATION_WORKLOAD_NAME}" \
      '.[] | select(.display_name == $name) | .id' \
      "${response_file}"
  )"

  local draft_yaml
  draft_yaml=$'receivers:\n  otlp:\n    protocols:\n      grpc: {}\nprocessors:\n  batch: {}\nexporters:\n  debug: {}\nservice:\n  pipelines:\n    traces:\n      receivers: [otlp]\n      processors: [batch]\n      exporters: [debug]\n'

  jq --null-input \
    --arg draft_yaml "${draft_yaml}" \
    '{draft_yaml: $draft_yaml, target_group: "activation", target_env: "dev", comment: "Activation smoke test request"}' \
    | curl --fail --silent --show-error \
      --cookie "${activation_cookie_jar}" \
      --header 'Content-Type: application/json' \
      --data-binary @- \
      --output "${response_file}" \
      "${ACTIVATION_API_URL}/api/workloads/${workload_id}/config/approvals"
  local approval_id
  approval_id="$(jq -er 'select(.status == "pending") | .id' "${response_file}")"

  jq --null-input '{comment: "Activation smoke test approval"}' \
    | curl --fail --silent --show-error \
      --cookie "${activation_cookie_jar}" \
      --header 'Content-Type: application/json' \
      --data-binary @- \
      --output "${response_file}" \
      "${ACTIVATION_API_URL}/api/workloads/${workload_id}/config/approvals/${approval_id}/approve"
  jq -e '.status == "approved"' "${response_file}" >/dev/null

  jq --null-input '{comment: "Activation smoke test governed push"}' \
    | curl --fail --silent --show-error \
      --cookie "${activation_cookie_jar}" \
      --header 'Content-Type: application/json' \
      --data-binary @- \
      --output "${response_file}" \
      "${ACTIVATION_API_URL}/api/workloads/${workload_id}/config/approvals/${approval_id}/push"
  jq -e '.status == "pushed"' "${response_file}" >/dev/null

  activation_wait_for_applied_config \
    "${activation_cookie_jar}" \
    "${workload_id}" \
    "${response_file}" \
    "${deadline}"

  local finished_at
  finished_at="$(date +%s)"
  local elapsed_seconds=$((finished_at - started_at))
  if ((elapsed_seconds >= ACTIVATION_TIMEOUT_SECONDS)); then
    echo "activation exceeded ${ACTIVATION_TIMEOUT_SECONDS} seconds" >&2
    return 1
  fi

  echo "activation smoke: OK"
  echo "health=ready features=approvals+policy_preview login=ok workload=connected governed_push=applied"
  echo "postgres_version=${postgres_version}"
  echo "activation_seconds=${elapsed_seconds}"
}

activation_main "$@"
