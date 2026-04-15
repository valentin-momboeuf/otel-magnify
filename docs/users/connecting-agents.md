# Connecting agents

Agents connect to otel-magnify over [OpAMP](https://opentelemetry.io/docs/specs/opamp/) on port `:4320` (configurable via `OPAMP_ADDR`).

Two agent types are supported:

- **OTel Collectors** — the standard `otelcol*` binaries.
- **SDK agents** — any application using the OpenTelemetry SDK with an OpAMP client.

Agent type is detected from the `service.name` reported in the `AgentDescription` message. Anything matching the `otelcol*` pattern is treated as a Collector; everything else as an SDK agent.

## Configuring an OTel Collector

Add an `opamp` extension to your Collector configuration and reference it in `service::extensions`:

```yaml
extensions:
  opamp:
    server:
      ws:
        endpoint: ws://magnify.example.com:4320/v1/opamp
    instance_uid: collector-prod-eu-01
    capabilities:
      reports_effective_config: true
      reports_remote_config: true
      reports_health: true
      accepts_remote_config: true

service:
  extensions: [opamp]
  pipelines:
    # ...
```

Sample configs are available in the repo under `agents/collector-*.yaml`.

## Running a demo Collector alongside otel-magnify

```bash
docker run -d --name collector-prod-eu --network otel-magnify_default \
  -v $(pwd)/agents/collector-prod-eu.yaml:/etc/otelcol-contrib/config.yaml \
  otel/opentelemetry-collector-contrib:0.98.0
```

## Simulating an SDK agent

For development and testing, the repo ships a small simulator at `backend/cmd/sdkagent/` that connects as a fake SDK agent.

```bash
cd backend
go run ./cmd/sdkagent/ --endpoint ws://localhost:4320/v1/opamp --name demo-sdk-agent
```

## What otel-magnify captures from an agent

- Identity: `service.name`, `service.version`, `service.instance.id`, labels.
- Effective configuration (what the agent currently runs).
- Remote configuration status (was the last push applied?).
- Available components — modules compiled into the agent, used to validate pushed configs against what the agent actually supports.
- Health — reported periodically, drives the alert engine.
