# OpAMP flow

This page walks through the full lifecycle of an agent connection and config push, from first handshake to auto-rollback on failure.

## Connection and description

```mermaid
sequenceDiagram
    autonumber
    participant A as Agent
    participant O as OpAMP server
    participant S as Store

    A->>O: WebSocket upgrade on /v1/opamp
    O->>A: Accept connection
    A->>O: AgentToServer{AgentDescription, Capabilities}
    O->>S: Upsert agent (identity, labels, version)
    O->>A: ServerToAgent{} (empty, acknowledges)
    loop heartbeat
        A->>O: AgentToServer{Health, EffectiveConfig?}
        O->>S: Update last-seen, effective config, health
    end
```

## Config push with success

```mermaid
sequenceDiagram
    autonumber
    participant U as UI
    participant API as REST API
    participant O as OpAMP server
    participant A as Agent
    participant S as Store
    participant WS as WebSocket hub

    U->>API: POST /api/agents/{id}/config (raw YAML)
    API->>API: Validate against AvailableComponents
    API->>S: Insert agent_configs row (status=pending)
    API->>O: Trigger push for {id}
    O->>A: ServerToAgent{RemoteConfig}
    A->>O: AgentToServer{RemoteConfigStatus: APPLYING → APPLIED}
    O->>S: Update row (status=applied)
    O->>WS: broadcast agent_config_status
    WS-->>U: live update
```

## Config push with failure and auto-rollback

```mermaid
sequenceDiagram
    autonumber
    participant U as UI
    participant O as OpAMP server
    participant A as Agent
    participant S as Store
    participant WS as WebSocket hub

    Note over O,A: A bad config was just pushed
    A->>O: AgentToServer{RemoteConfigStatus: FAILED, error}
    O->>S: Update row (status=failed, error_message)
    O->>S: Load last-applied config (GetLastAppliedAgentConfig)
    O->>A: ServerToAgent{RemoteConfig: last-good}
    O->>S: Insert new row (status=pending, pushed_by=auto-rollback)
    O->>WS: broadcast auto_rollback_applied
    A->>O: AgentToServer{RemoteConfigStatus: APPLIED}
    O->>S: Update rollback row (status=applied)
    WS-->>U: live update
```

## Available components capture

When an agent connects, it advertises the modules compiled into it via `AvailableComponents`. otel-magnify persists this and uses it to validate config pushes before sending them — rejecting configs that reference receivers, processors, or exporters the agent cannot run.
