# OpAMP endpoint

otel-magnify accepts OpAMP client connections on a dedicated WebSocket listener. The default endpoint is:

```text
wss://<host>:4320/v1/opamp
```

The listen address is configured with `OPAMP_ADDR`; the path is always
`/v1/opamp`. The listener is disabled by default until native TLS is configured.

## Authentication and transport

Every handshake requires an active managed token:

```http
Authorization: Bearer <managed-opamp-token>
```

Create, list, rotate, and revoke tokens through the versioned admin API or the
**Administration → OpAMP tokens** page. The credential value is returned only
once. See [Managed OpAMP tokens](../users/opamp-tokens.md) for the complete
lifecycle and client configuration.

Missing, malformed, unknown, expired, and revoked tokens receive the same
generic `401 Unauthorized` with
`WWW-Authenticate: Bearer realm="opamp"`. A token-store failure returns generic
`503 Service Unavailable`. Successful authentication does not bind the token's
team or environment metadata to the workload attributes reported by the
client.

Native TLS uses `OPAMP_TLS_CERT_FILE` and `OPAMP_TLS_KEY_FILE`. With the
default `OPAMP_INSECURE=false`, missing or invalid TLS material keeps the
OpAMP listener disabled. `OPAMP_INSECURE=true` enables plaintext `ws://` only
for an explicitly trusted local test network and cannot be combined with TLS
files. Production load balancers must pass through TLS or re-encrypt to the
native WSS listener.

## Capabilities used by otel-magnify

| Capability | How it is used |
|------------|----------------|
| `ReportsEffectiveConfig` | Tracks the effective config hash reported by each instance. |
| `ReportsRemoteConfig` / `AcceptsRemoteConfig` | Records whether the workload can accept remote config; the UI/API gate push affordances and return `409 remote_config_unsupported` when a workload cannot accept it. |
| `ReportsHealth` | Updates live instance health and workload status. |
| `ReportsAvailableComponents` | Captures Collector receivers/processors/exporters/extensions and feeds config validation. |

Remote config status error messages are sanitized before they are persisted, broadcast, or rendered. See [Remote config status redaction](../security/remote-config-status-redaction.md) for the boundary.

## Workload identity

OpAMP `AgentDescription` resource attributes are mapped into otel-magnify workloads. Fingerprinting prefers Kubernetes workload identity, then host/service identity, then instance UID. See [Workload identity](../users/connecting-agents.md#workload-identity) for the exact attribute strategies.

Agent type detection uses `service.name` patterns: Collector-like names such as `otelcol*` are shown as collectors, while other OpAMP clients are shown as SDK agents.
