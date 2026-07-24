# Connecting agents

Agents connect to otel-magnify over [OpAMP](https://opentelemetry.io/docs/specs/opamp/) on port `:4320` (configurable via `OPAMP_ADDR`).

Two agent types are supported:

- **OTel Collectors** — the standard `otelcol*` binaries.
- **SDK agents** — any application using the OpenTelemetry SDK with an OpAMP client.

Agent type is detected from the `service.name` reported in the `AgentDescription` message. Anything matching the `otelcol*` pattern is treated as a Collector; everything else as an SDK agent.

## Workload identity

otel-magnify groups connected agents into **workloads** — a Kubernetes Deployment, DaemonSet, StatefulSet, Job, or CronJob for K8s-native collectors, or a single host/process for anything else. The workload is the unit you see in the inventory and the unit that carries the active configuration; individual pods are shown as *instances* of that workload.

The identity is derived from resource attributes on the OpAMP `AgentDescription`:

| Strategy | Attributes used |
|---|---|
| **k8s** | `k8s.cluster.name` (defaults to `unknown`) + `k8s.namespace.name` + one of `k8s.deployment.name` / `k8s.daemonset.name` / `k8s.statefulset.name` / `k8s.job.name` / `k8s.cronjob.name` |
| **host** | `service.name` + `host.name` |
| **uid** | fallback on the OpAMP `InstanceUid` (cardinality 1 per process) |

The first strategy that can be satisfied is used. For a Kubernetes collector, enable the `resourcedetection` processor with the `k8s` detector to populate the required attributes automatically.

### Pod lifecycle

- When a new pod of an existing workload connects, the server auto-pushes the workload's active config if the pod's effective config diverges (P.2 semantics).
- When the last pod of a workload disconnects, the workload stays `connected` for a grace window (`WORKLOAD_DISCONNECT_GRACE_SECONDS`, default 120 s). After the grace it flips to `disconnected` and starts its retention countdown (`WORKLOAD_RETENTION_DAYS`, default 30 days), at the end of which the workload is archived.
- Every pod connect, disconnect, and `service.version` change is recorded in an append-only `workload_events` log (`WORKLOAD_EVENT_RETENTION_DAYS`, default 30 days). The Activity tab on the workload detail page renders this log — a noisy churn rate is a signal of a Kubernetes problem (CrashLoopBackOff, OOMKill, eviction storms).

!!! note "Migration from `/api/agents`"
    The legacy `/api/agents/*` endpoints still resolve — they reply with HTTP `307 Temporary Redirect` to their `/api/workloads/*` equivalent. Existing integrations keep working; new integrations should call `/api/workloads/*` directly.

## Configuring an OTel Collector

Add an `opamp` extension to your Collector configuration and reference it in `service::extensions`:

```yaml
extensions:
  opamp:
    server:
      ws:
        endpoint: wss://magnify.example.com/v1/opamp
        headers:
          Authorization: "Bearer ${env:OPAMP_TOKEN}"
        tls:
          insecure: false

service:
  extensions: [opamp]
  pipelines:
    # ...
```

This built-in extension provides inventory and status reporting. It does not
apply remote configuration, so otel-magnify records
`accepts_remote_config=false` and rejects governed pushes to this workload.
Use the OpAMP Supervisor below for a real Collector that must apply config.

`OPAMP_TOKEN` is a client environment variable, not a server setting. Inject it
from an operator-managed secret; never commit a value. For private PKI, mount
the CA and set `tls.ca_file` while keeping `insecure: false`. See
[Managed OpAMP tokens](opamp-tokens.md) for bootstrap, Kubernetes, rotation,
and failure semantics.

Sample configs are available in the repo under `agents/collector-*.yaml`. They
target Collector contrib `0.150.1` and ship with the `resourcedetection` and
`resource` processors pre-wired so the collector is fingerprinted correctly.

## Running a demo Collector alongside otel-magnify

```bash
docker run -d --name collector-prod-eu --network otel-magnify_default \
  --env-file /secure/path/opamp-client.env \
  -v $(pwd)/agents/collector-prod-eu.yaml:/etc/otelcol-contrib/config.yaml \
  otel/opentelemetry-collector-contrib:0.150.1
```

The private environment file contains `OPAMP_TOKEN` and should be mode `0600`.
The public sample endpoint must be replaced with the WSS name on the server
certificate.

## Running a Collector via OpAMP Supervisor

The Collector's built-in `opamp` extension reports status and effective config,
but **does not apply remote configs**. To enable config push, run the Collector
under the [OpAMP Supervisor](https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/cmd/opampsupervisor).

Official Supervisor binaries and container images are available. This
documentation fixes Supervisor `0.150.0` with Collector contrib `0.150.1`;
do not silently change either version because the configuration is
version-sensitive. The official image is:

```text
ghcr.io/open-telemetry/opentelemetry-collector-releases/opentelemetry-collector-opampsupervisor:0.150.0
```

The official Supervisor image also bundles the Collector from the Supervisor
release. The documented otel-magnify pairing deliberately uses Collector
contrib `0.150.1`, so build a pinned combined image around the Supervisor
`0.150.0` binary or mount a verified `0.150.1` executable through your artifact
workflow. Do not silently use the bundled different version or download an
unpinned executable during pod startup.

Supervisor configuration (`supervisor.yaml`):

```yaml
server:
  endpoint: wss://magnify.example.com/v1/opamp
  headers:
    Authorization: "Bearer ${env:OPAMP_TOKEN}"
  tls:
    insecure: false

capabilities:
  accepts_remote_config: true     # required for config push
  reports_effective_config: true
  reports_health: true
  reports_remote_config: true

agent:
  executable: /otelcol-contrib    # path inside the contrib image
  description:
    identifying_attributes:
      service.name: otelcol-contrib    # must match otelcol* to be classified as a collector
      service.version: 0.150.1
      service.instance.id: collector-supervised-eu
    non_identifying_attributes:
      deployment.environment: production

storage:
  directory: /tmp/supervisor       # needs a writable dir inside the container
```

Inject `OPAMP_TOKEN` from a Kubernetes `secretKeyRef`, External Secrets, Vault,
or an equivalent secret manager. Mount a private CA and set
`server.tls.ca_file` when the WSS certificate is not rooted in the system trust
store. Give the non-root runtime a writable Supervisor storage volume; do not
solve storage permissions by running the container as root.

## Simulating an SDK agent

For development and testing, the repo ships a small OpAMP simulator at
`cmd/sdkagent/`. By default it reports status only. The
`--accept-remote-config` flag opts into acknowledging remote config as applied;
it does not launch or reconfigure a Collector process.

```bash
go run ./cmd/sdkagent/ \
  --endpoint ws://localhost:4320/v1/opamp \
  --token-file /secure/path/opamp-token \
  --allow-insecure-transport \
  --name otelcol-activation-demo \
  --accept-remote-config
```

The reproducible local activation path starts this simulator through an
isolated Compose profile and checks the full governed flow:

```bash
./scripts/activation-smoke.sh
```

Use the Supervisor, not this simulator, to prove that a production Collector
can restart or reload with a candidate configuration.

## What otel-magnify captures from an agent

- Identity: `service.name`, `service.version`, `service.instance.id`, labels, plus the K8s/host resource attributes used to fingerprint the workload.
- Effective configuration (what the agent currently runs).
- Remote configuration status (was the last push applied?).
- Available components — modules compiled into the agent, used to validate pushed configs against what the agent actually supports.
- Health — reported periodically, drives the alert engine.
