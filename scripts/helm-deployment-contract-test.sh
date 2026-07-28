#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
chart_path="$repo_root/helm/otel-magnify"
temporary_directory="$(mktemp -d)"
failure_count=0

helm_contract_cleanup() {
  if [[ -n "${temporary_directory:-}" ]]; then
    rm -rf -- "$temporary_directory"
    temporary_directory=""
  fi
}
trap helm_contract_cleanup EXIT

helm_contract_record_failure() {
  echo "helm deployment contract test: FAIL: $*" >&2
  failure_count=$((failure_count + 1))
}

helm_contract_render() {
  local output_file="$1"
  shift

  if ! helm template magnify "$chart_path" \
    --namespace observability \
    "$@" \
    >"$output_file" 2>"$temporary_directory/render-error"; then
    cat "$temporary_directory/render-error" >&2
    exit 1
  fi
}

helm_contract_assert_fragment() {
  local rendered_file="$1"
  local expected="$2"
  local description="$3"
  local rendered

  rendered="$(<"$rendered_file")"
  if [[ "$rendered" != *"$expected"* ]]; then
    helm_contract_record_failure "$description"
  fi
}

helm_contract_assert_absent() {
  local rendered_file="$1"
  local unexpected="$2"
  local description="$3"

  if grep -Fq -- "$unexpected" "$rendered_file"; then
    helm_contract_record_failure "$description"
  fi
}

helm_contract_extract_network_policy() {
  local rendered_file="$1"

  awk '
    $0 == "kind: NetworkPolicy" {
      in_network_policy = 1
    }
    in_network_policy && $0 == "---" {
      exit
    }
    in_network_policy {
      print
    }
  ' "$rendered_file"
}

helm_contract_assert_render_fails() {
  local description="$1"
  local expected_error="$2"
  shift 2
  local error_file="$temporary_directory/expected-render-error"
  local output_file="$temporary_directory/unexpected-render-output"

  if helm template magnify "$chart_path" \
    --namespace observability \
    "$@" \
    >"$output_file" 2>"$error_file"; then
    helm_contract_record_failure "$description rendered successfully"
  elif ! grep -Fq -- "$expected_error" "$error_file"; then
    helm_contract_record_failure "$description did not return the required error"
  fi
}

helm_contract_assert_deployment_exact_line() {
  local rendered_file="$1"
  local expected_line="$2"
  local description="$3"

  if ! awk -v expected="$expected_line" '
    $0 == "kind: Deployment" {
      in_deployment = 1
      found_deployment = 1
    }
    in_deployment && $0 == expected {
      matches += 1
    }
    in_deployment && $0 == "---" {
      exit
    }
    END {
      if (!found_deployment || matches != 1) {
        exit 1
      }
    }
  ' "$rendered_file"; then
    helm_contract_record_failure "$description"
  fi
}

helm_contract_extract_checksum() {
  local rendered_file="$1"

  awk '$1 == "checksum/release-secret:" { print $2; exit }' "$rendered_file"
}

helm_contract_extract_probe() {
  local rendered_file="$1"
  local probe_name="$2"

  awk -v target="$probe_name:" '
    $0 == "          " target {
      in_probe = 1
    }
    in_probe {
      if (printed && length($0) > 0) {
        match($0, /[^ ]/)
        indentation = RSTART - 1
        if (indentation <= 10) {
          exit
        }
      }
      print
      printed = 1
    }
  ' "$rendered_file"
}

helm_contract_assert_probe() {
  local rendered_file="$1"
  local probe_name="$2"
  local expected_path="$3"
  local expected_period="$4"
  local expected_failure_threshold="$5"
  local expected_probe actual_probe

  expected_probe="          $probe_name:
            httpGet:
              path: $expected_path
              port: api
            periodSeconds: $expected_period
            failureThreshold: $expected_failure_threshold
            timeoutSeconds: 1"
  actual_probe="$(helm_contract_extract_probe "$rendered_file" "$probe_name")"

  if [[ "$actual_probe" != "$expected_probe" ]]; then
    helm_contract_record_failure "$probe_name does not match the required $expected_path timing contract"
  fi
}

external_render="$temporary_directory/external.yaml"
explicit_one_render="$temporary_directory/explicit-one.yaml"
revision_render="$temporary_directory/revision.yaml"
inline_a_render="$temporary_directory/inline-a.yaml"
inline_b_render="$temporary_directory/inline-b.yaml"
external_ignored_a_render="$temporary_directory/external-ignored-a.yaml"
external_ignored_b_render="$temporary_directory/external-ignored-b.yaml"
tls_render="$temporary_directory/tls.yaml"
insecure_render="$temporary_directory/insecure.yaml"
network_policy_disabled_render="$temporary_directory/network-policy-disabled.yaml"
network_policy_from_render="$temporary_directory/network-policy-from.yaml"
network_policy_allow_all_render="$temporary_directory/network-policy-allow-all.yaml"

external_secret_values=(
  --set database.existingSecret=magnify-db
  --set auth.existingSecret=magnify-auth
)

helm_contract_render "$external_render" "${external_secret_values[@]}"
helm_contract_render "$explicit_one_render" \
  "${external_secret_values[@]}" \
  --set replicaCount=1

helm_contract_assert_deployment_exact_line \
  "$external_render" \
  "  replicas: 1" \
  "default values do not render exactly one Deployment replicas: 1 line"
helm_contract_assert_deployment_exact_line \
  "$explicit_one_render" \
  "  replicas: 1" \
  "--set replicaCount=1 does not render exactly one Deployment replicas: 1 line"
helm_contract_assert_fragment \
  "$external_render" \
  $'  strategy:\n    type: Recreate' \
  "Deployment strategy is not Recreate"

helm_contract_assert_fragment \
  "$external_render" \
  $'            - name: OPAMP_INSECURE\n              value: "false"' \
  "default transport does not render OPAMP_INSECURE=false"
for unexpected_tls_default in \
  "OPAMP_TLS_CERT_FILE" \
  "OPAMP_TLS_KEY_FILE" \
  "opamp-tls"; do
  helm_contract_assert_absent \
    "$external_render" \
    "$unexpected_tls_default" \
    "default transport unexpectedly renders TLS material: $unexpected_tls_default"
done

helm_contract_assert_probe "$external_render" startupProbe /healthz 5 60
helm_contract_assert_probe "$external_render" livenessProbe /healthz 10 3
helm_contract_assert_probe "$external_render" readinessProbe /readyz 5 3

while IFS='|' read -r fixture_name replica_display replica_yaml_value; do
  replica_values_file="$temporary_directory/replica-${fixture_name}.yaml"
  replica_error="$temporary_directory/replica-${fixture_name}.error"
  printf 'replicaCount: %s\n' "$replica_yaml_value" >"$replica_values_file"

  if helm template magnify "$chart_path" \
    --namespace observability \
    "${external_secret_values[@]}" \
    --values "$replica_values_file" \
    >"$temporary_directory/replica-${fixture_name}.rendered.yaml" 2>"$replica_error"; then
    helm_contract_record_failure "replicaCount=$replica_display rendered successfully"
  elif ! grep -Fq \
    "replicaCount must remain 1 because OpAMP connections, token revocation, and token expiration are process-local; distributed broadcast is required before scaling out" \
    "$replica_error"; then
    helm_contract_record_failure "replicaCount=$replica_display did not return the required error"
  fi
done <<'EOF'
fractional-1-1|1.1|1.1
fractional-1-5|1.5|1.5
fractional-1-9|1.9|1.9
boolean-true|true|true
string-one|"1"|"1"
integer-zero|0|0
integer-two|2|2
EOF

helm_contract_render "$tls_render" \
  "${external_secret_values[@]}" \
  --set opamp.tls.existingSecret=magnify-opamp-tls
for tls_setting in \
  $'            - name: OPAMP_INSECURE\n              value: "false"' \
  $'            - name: OPAMP_TLS_CERT_FILE\n              value: "/var/run/otel-magnify/opamp-tls/tls.crt"' \
  $'            - name: OPAMP_TLS_KEY_FILE\n              value: "/var/run/otel-magnify/opamp-tls/tls.key"' \
  $'            - name: opamp-tls\n              mountPath: /var/run/otel-magnify/opamp-tls\n              readOnly: true' \
  $'        - name: opamp-tls\n          secret:\n            secretName: magnify-opamp-tls\n            items:\n              - key: "tls.crt"\n                path: tls.crt\n              - key: "tls.key"\n                path: tls.key'; do
  helm_contract_assert_fragment \
    "$tls_render" \
    "$tls_setting" \
    "external OpAMP TLS Secret does not render the required setting: ${tls_setting%%$'\n'*}"
done

helm_contract_render "$insecure_render" \
  "${external_secret_values[@]}" \
  --set opamp.insecure=true
helm_contract_assert_fragment \
  "$insecure_render" \
  $'            - name: OPAMP_INSECURE\n              value: "true"' \
  "explicit insecure transport does not render OPAMP_INSECURE=true"
for unexpected_tls_insecure in \
  "OPAMP_TLS_CERT_FILE" \
  "OPAMP_TLS_KEY_FILE" \
  "opamp-tls"; do
  helm_contract_assert_absent \
    "$insecure_render" \
    "$unexpected_tls_insecure" \
    "explicit insecure transport unexpectedly renders TLS material: $unexpected_tls_insecure"
done

helm_contract_assert_render_fails \
  "opamp.insecure=true with opamp.tls.existingSecret" \
  "opamp.insecure=true cannot be combined with opamp.tls.existingSecret" \
  "${external_secret_values[@]}" \
  --set opamp.insecure=true \
  --set opamp.tls.existingSecret=magnify-opamp-tls

helm_contract_assert_render_fails \
  "opamp.insecure string false" \
  "opamp.insecure must be a boolean" \
  "${external_secret_values[@]}" \
  --set-string opamp.insecure=false \
  --set opamp.tls.existingSecret=magnify-opamp-tls
helm_contract_assert_render_fails \
  "networkPolicy.enabled string false" \
  "networkPolicy.enabled must be a boolean" \
  "${external_secret_values[@]}" \
  --set-string networkPolicy.enabled=false
helm_contract_assert_render_fails \
  "networkPolicy.api.allowAll string false" \
  "networkPolicy.api.allowAll must be a boolean" \
  "${external_secret_values[@]}" \
  --set-string networkPolicy.api.allowAll=false
helm_contract_assert_render_fails \
  "networkPolicy.opamp.allowAll string false" \
  "networkPolicy.opamp.allowAll must be a boolean" \
  "${external_secret_values[@]}" \
  --set-string networkPolicy.opamp.allowAll=false

default_network_policy="$(helm_contract_extract_network_policy "$external_render")"
expected_default_network_policy='kind: NetworkPolicy
metadata:
  name: magnify
  namespace: observability
  labels:
    app: magnify
spec:
  podSelector:
    matchLabels:
      app: magnify
  policyTypes:
    - Ingress
  ingress:
    - ports:
        - port: api'
if [[ "$default_network_policy" != "$expected_default_network_policy" ]]; then
  helm_contract_record_failure "default NetworkPolicy does not allow only the API port"
fi

helm_contract_render "$network_policy_disabled_render" \
  "${external_secret_values[@]}" \
  --set networkPolicy.enabled=false
helm_contract_assert_absent \
  "$network_policy_disabled_render" \
  "kind: NetworkPolicy" \
  "networkPolicy.enabled=false still renders a NetworkPolicy"

network_policy_values="$temporary_directory/network-policy-values.yaml"
printf '%s\n' \
  'networkPolicy:' \
  '  api:' \
  '    allowAll: false' \
  '  opamp:' \
  '    from:' \
  '      - namespaceSelector:' \
  '          matchLabels:' \
  '            kubernetes.io/metadata.name: agents' \
  '        podSelector:' \
  '          matchLabels:' \
  '            app.kubernetes.io/name: collector' \
  >"$network_policy_values"
helm_contract_render "$network_policy_from_render" \
  "${external_secret_values[@]}" \
  --values "$network_policy_values"
network_policy_from="$(helm_contract_extract_network_policy "$network_policy_from_render")"
expected_network_policy_from='kind: NetworkPolicy
metadata:
  name: magnify
  namespace: observability
  labels:
    app: magnify
spec:
  podSelector:
    matchLabels:
      app: magnify
  policyTypes:
    - Ingress
  ingress:
    - from:
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: agents
          podSelector:
            matchLabels:
              app.kubernetes.io/name: collector
      ports:
        - port: opamp'
if [[ "$network_policy_from" != "$expected_network_policy_from" ]]; then
  helm_contract_record_failure "OpAMP NetworkPolicy peers do not render exactly on the named opamp port"
fi

helm_contract_render "$network_policy_allow_all_render" \
  "${external_secret_values[@]}" \
  --set networkPolicy.api.allowAll=false \
  --set networkPolicy.opamp.allowAll=true
network_policy_allow_all="$(helm_contract_extract_network_policy "$network_policy_allow_all_render")"
expected_network_policy_allow_all='kind: NetworkPolicy
metadata:
  name: magnify
  namespace: observability
  labels:
    app: magnify
spec:
  podSelector:
    matchLabels:
      app: magnify
  policyTypes:
    - Ingress
  ingress:
    - ports:
        - port: opamp'
if [[ "$network_policy_allow_all" != "$expected_network_policy_allow_all" ]]; then
  helm_contract_record_failure "explicit OpAMP allowAll does not render a port-only ingress rule"
fi

helm_contract_render "$inline_a_render" \
  --set database.existingSecret=magnify-db \
  --set-string jwtSecret=synthetic-jwt-secret-a
helm_contract_render "$inline_b_render" \
  --set database.existingSecret=magnify-db \
  --set-string jwtSecret=synthetic-jwt-secret-b

checksum_a="$(helm_contract_extract_checksum "$inline_a_render")"
checksum_b="$(helm_contract_extract_checksum "$inline_b_render")"
if [[ -z "$checksum_a" || -z "$checksum_b" ]]; then
  helm_contract_record_failure "checksum/release-secret is missing for an inline release Secret"
elif [[ "$checksum_a" == "$checksum_b" ]]; then
  helm_contract_record_failure "checksum/release-secret did not change with a consumed inline Secret"
fi

helm_contract_render "$external_ignored_a_render" \
  "${external_secret_values[@]}" \
  --set-string database.dsn=synthetic-ignored-dsn-a \
  --set-string jwtSecret=synthetic-ignored-jwt-a
helm_contract_render "$external_ignored_b_render" \
  "${external_secret_values[@]}" \
  --set-string database.dsn=synthetic-ignored-dsn-b \
  --set-string jwtSecret=synthetic-ignored-jwt-b

external_checksum_a="$(helm_contract_extract_checksum "$external_ignored_a_render")"
external_checksum_b="$(helm_contract_extract_checksum "$external_ignored_b_render")"
if [[ -z "$external_checksum_a" || -z "$external_checksum_b" ]]; then
  helm_contract_record_failure "checksum/release-secret is missing for external Secret references"
elif [[ "$external_checksum_a" != "$external_checksum_b" ]]; then
  helm_contract_record_failure "ignored legacy inline values changed checksum/release-secret"
fi

for ignored_fixture in \
  synthetic-ignored-dsn-a \
  synthetic-ignored-jwt-a \
  synthetic-ignored-dsn-b \
  synthetic-ignored-jwt-b; do
  helm_contract_assert_absent \
    "$external_ignored_a_render" \
    "$ignored_fixture" \
    "external Secret render contains ignored fixture: $ignored_fixture"
  helm_contract_assert_absent \
    "$external_ignored_b_render" \
    "$ignored_fixture" \
    "external Secret render contains ignored fixture: $ignored_fixture"
done

helm_contract_render "$revision_render" \
  "${external_secret_values[@]}" \
  --set-string deployment.secretRevision=rev-2
helm_contract_assert_fragment \
  "$revision_render" \
  'otel-magnify.io/secret-revision: "rev-2"' \
  "deployment.secretRevision is not rendered in the pod template"

if grep -R -Fq "lookup" "$chart_path/templates"; then
  helm_contract_record_failure "chart templates use lookup for an external Secret"
fi

for pool_setting in \
  $'            - name: DB_MAX_OPEN_CONNS\n              value: "40"' \
  $'            - name: DB_MAX_IDLE_CONNS\n              value: "10"' \
  $'            - name: DB_CONN_MAX_IDLE_TIME_SECONDS\n              value: "300"' \
  $'            - name: DB_CONN_MAX_LIFETIME_SECONDS\n              value: "1800"'; do
  helm_contract_assert_fragment \
    "$external_render" \
    "$pool_setting" \
    "missing or incorrect database pool environment setting: ${pool_setting%%$'\n'*}"
done

if ((failure_count > 0)); then
  echo "helm deployment contract test: $failure_count failure(s)" >&2
  exit 1
fi

echo "helm deployment contract test: OK"
