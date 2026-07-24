# Managed OpAMP tokens

Every Collector or Supervisor connection to otel-magnify must authenticate with
a managed OpAMP token. Tokens are machine credentials, separate from browser
JWTs, the server TLS private key, and Collector exporter credentials.

## Bootstrap the first connection

Start the API and frontend before starting any agent. The OpAMP listener is
disabled unless its transport is configured, but the API remains available so
an administrator can sign in and create the first token.

In the UI, open **Administration → OpAMP tokens**, select **Create token**, and
set a descriptive name plus optional team, environment, description, and
expiry. The same operation is available through
`POST /api/v1/opamp/tokens` to callers with `settings:manage`.

The credential is returned once, in the form
`ompt_<token-id>.<opaque-secret>`. The server stores only its hash and public
metadata. Save the complete value immediately in the operator's secret manager;
closing the UI dialog or losing the create response is irreversible. Do not
paste it into a values file, source file, issue, log, or shell history.

Create separate tokens for teams, environments, and operational boundaries.
Multiple active tokens make ownership visible and permit overlap during
rotation. A token's `team` and `environment` fields are labels for operators;
they do not authorize or bind a connection to reported workload metadata.

## Configure the server transport

Production OpAMP uses native TLS. The server defaults to
`OPAMP_INSECURE=false`; without both `OPAMP_TLS_CERT_FILE` and
`OPAMP_TLS_KEY_FILE`, the OpAMP listener stays disabled while the API continues
to run. Supply the certificate and key from an external secret:

```text
OPAMP_INSECURE=false
OPAMP_TLS_CERT_FILE=/var/run/otel-magnify/opamp-tls/tls.crt
OPAMP_TLS_KEY_FILE=/var/run/otel-magnify/opamp-tls/tls.key
```

For Helm, reference the operator-managed TLS Secret:

```yaml
opamp:
  insecure: false
  tls:
    existingSecret: magnify-opamp-tls
```

The chart mounts the Secret read-only and sets the two file variables. Keep
this **server TLS Secret** distinct from every **client token Secret**. The
chart's HTTP Ingress exposes only the API/frontend; expose the native WSS port
through TCP/TLS passthrough or a load balancer that re-encrypts to the native
listener. Restrict source namespaces, pods, or networks with
`networkPolicy.opamp.from`; the chart denies OpAMP ingress by default.

Plain `ws://` is only for an explicitly trusted local test network. Set
`OPAMP_INSECURE=true` with no TLS files, as the repository's isolated Compose
scenario does. Never transmit a bearer token over plaintext outside that
bounded local mode, and never disable client certificate verification.

## Configure clients

`OPAMP_TOKEN` is the client-side environment variable used by these examples.
It is not a server environment variable.

### Direct Collector

The checked examples target Collector contrib `0.150.1`. Preserve this exact
OpAMP connection block while adapting the endpoint:

```yaml
server:
  ws:
    endpoint: wss://magnify.example.com/v1/opamp
    headers:
      Authorization: "Bearer ${env:OPAMP_TOKEN}"
    tls:
      insecure: false
```

Place it under `extensions::opamp`, and include `opamp` in
`service::extensions`. The direct Collector extension reports inventory and
status but does not apply remote configuration.

For a private PKI, mount the CA and add `ca_file`; keep verification enabled:

```yaml
server:
  ws:
    endpoint: wss://magnify.example.com/v1/opamp
    headers:
      Authorization: "Bearer ${env:OPAMP_TOKEN}"
    tls:
      insecure: false
      ca_file: /var/run/secrets/opamp-ca/ca.crt
```

### OpAMP Supervisor

Use the official OpAMP Supervisor `0.150.0` when the Collector must apply
remote configuration. This version has official binaries and the image
`ghcr.io/open-telemetry/opentelemetry-collector-releases/opentelemetry-collector-opampsupervisor:0.150.0`.
Pair it with Collector contrib `0.150.1`; do not silently substitute another
version because the Supervisor configuration is version-sensitive. The
official Supervisor image bundles the Collector from its own release, so use
the `0.150.0` Supervisor binary in a pinned combined image or mount a verified
Collector contrib `0.150.1` executable.

The Supervisor's equivalent connection block is:

```yaml
server:
  endpoint: wss://magnify.example.com/v1/opamp
  headers:
    Authorization: "Bearer ${env:OPAMP_TOKEN}"
  tls:
    insecure: false
```

Set `capabilities.accepts_remote_config: true` and provide a writable
`storage.directory`. For a private PKI, mount the CA and add
`server.tls.ca_file` rather than disabling verification.

### Kubernetes

Have External Secrets, Vault, or another operator-managed workflow populate the
client token Secret. Reference it from the Collector or Supervisor pod; do not
copy its value into a manifest:

```yaml
env:
  - name: OPAMP_TOKEN
    valueFrom:
      secretKeyRef:
        name: opamp-client-token
        key: token
```

Mount a private CA from a separate Secret when required:

```yaml
volumeMounts:
  - name: opamp-ca
    mountPath: /var/run/secrets/opamp-ca
    readOnly: true
volumes:
  - name: opamp-ca
    secret:
      secretName: opamp-client-ca
      defaultMode: 0400
```

Ensure the container UID can read mounted files. Outside Kubernetes, store
token and private-CA files in an operator-owned directory with mode `0700` and
credential files with mode `0600`. Avoid world-readable files, process
arguments, and committed environment files.

## Rotate, expire, and revoke

Use overlapping rotation:

1. Create a replacement token and store its one-shot value.
2. Deploy it to the intended clients while the old token remains active.
3. Refresh the token list until the replacement's `last_used_at` proves a
   client completed its first authenticated OpAMP message.
4. Revoke the old token.

Revocation prevents new handshakes and disconnects existing connections that
used that token. Expiration has the same fail-closed connection effect at
`expires_at`: new handshakes are rejected and existing connections are
disconnected. Clients then reconnect only after receiving another active token.

## Failure and reconciliation semantics

- Invalid, unknown, expired, and revoked credentials all receive the same
  generic `401 Unauthorized` response with
  `WWW-Authenticate: Bearer realm="opamp"`.
- A token-store failure during the handshake returns generic
  `503 Service Unavailable`.
- Create and revoke are committed atomically with their token lifecycle audit
  event. If the database commit result cannot be known, the admin API returns
  `503`, `side_effect_status: "unknown"`, and the public `token_id`.
- After an unknown create, list tokens and revoke the exact returned ID if it
  exists; its one-shot value was not delivered safely. After an unknown revoke,
  list the token and retry revocation only if it is still active or expired.
  Do not blindly retry a create.

Community persists a durable audit outbox only for token creation and
revocation. When an Enterprise audit sink is configured, delivery is at least
once: a sink can receive the same `event_id` again if delivery succeeded but
acknowledgement did not. Enterprise consumers must deduplicate by `event_id`.

## Security boundaries and current limits

Managed tokens authenticate a connection but do not authorize a team,
environment, workload name, or resource attribute. A holder can report spoofed
workload metadata. Do not treat inventory metadata as a strong workload
identity, and never place raw secrets in remote Collector configurations:
remote configs are stored, rendered, and delivered as configuration data.

Community supports one server replica. Token revocation, token expiration, live
connections, and invalidation are process-local, so the Helm chart rejects any
`replicaCount` other than `1` and uses the `Recreate` strategy. Distributed
invalidation is required before horizontal scaling is safe.

There is intentionally no application rate limiter keyed by `RemoteAddr`.
Behind a proxy or NAT, that would let one abusive connection lock out an entire
fleet. Apply connection and request controls only at a trusted edge that has an
authoritative client source, and preserve generic server responses.

Per-instance credentials and stronger workload identity with mTLS or
SPIFFE/SPIRE are deliberately deferred. Until that mode exists, scope tokens
by operational boundary, rotate them, restrict network reachability, and
monitor `last_used_at`.
