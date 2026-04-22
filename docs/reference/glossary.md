# Glossary

**Agent**
: A process that speaks OpAMP — either an OTel Collector (`otelcol*`) or an SDK agent using the OpenTelemetry SDK. One or more agents connect as **instances** of a **workload**.

**Available components**
: The receivers, processors, exporters, and extensions compiled into an agent. Reported to otel-magnify via OpAMP and used to validate pushed configs.

**Collector**
: The OpenTelemetry Collector binary (`otelcol`, `otelcol-contrib`, ...). Handles telemetry ingestion and forwarding.

**Config hash**
: SHA-256 hash of a config's content. Used as the stable identity of a pushed configuration in the workload push history and OpAMP messages.

**Effective configuration**
: What an agent is actually running right now. The agent reports it back to otel-magnify whenever it changes.

**Fingerprint**
: Stable identity derived from OpAMP resource attributes that groups pods into a logical workload. Three strategies (`k8s`, `host`, `uid`) selected first-match.

**Instance**
: A single OpAMP-connected pod (or process) belonging to a workload. Lives in the server's in-memory registry only; not persisted.

**OpAMP**
: The [Open Agent Management Protocol](https://opentelemetry.io/docs/specs/opamp/). A WebSocket-based protocol for managing agents from a central server.

**Remote configuration status**
: The outcome of the last remote config push. One of `APPLIED`, `FAILED`, `PENDING`.

**SDK agent**
: Any application using an OpenTelemetry SDK and an OpAMP client. Distinguished from Collectors by its `service.name`.

**Signal Deck**
: The otel-magnify frontend design system — warm gold accent, Plus Jakarta Sans + Fira Code.

**Workload**
: A logical unit of management — a Kubernetes Deployment, DaemonSet, StatefulSet, Job, CronJob, or a single host/process for non-K8s agents. The entity the inventory lists, configs are pushed to, and retention applies to.

**Workload event**
: An append-only record of a pod lifecycle transition (connect / disconnect / version change). Persisted in the `workload_events` table; queried by the Activity tab.
