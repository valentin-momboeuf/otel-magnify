# Glossary

**Agent**
: A process that speaks OpAMP — either an OTel Collector (`otelcol*`) or an SDK agent using the OpenTelemetry SDK.

**Available components**
: The receivers, processors, exporters, and extensions compiled into an agent. Reported to otel-magnify via OpAMP and used to validate pushed configs.

**Collector**
: The OpenTelemetry Collector binary (`otelcol`, `otelcol-contrib`, ...). Handles telemetry ingestion and forwarding.

**Config hash**
: SHA-256 hash of a config's content. Used as the stable identity of a pushed configuration in the push history and OpAMP messages.

**Effective configuration**
: What an agent is actually running right now. The agent reports it back to otel-magnify whenever it changes.

**OpAMP**
: The [Open Agent Management Protocol](https://opentelemetry.io/docs/specs/opamp/). A WebSocket-based protocol for managing agents from a central server.

**Remote configuration status**
: The outcome of the last remote config push. One of `APPLIED`, `FAILED`, `PENDING`.

**SDK agent**
: Any application using an OpenTelemetry SDK and an OpAMP client. Distinguished from Collectors by its `service.name`.

**Signal Deck**
: The otel-magnify frontend design system — warm gold accent, Plus Jakarta Sans + Fira Code.
