# Architecture

otel-magnify is a single Go binary that embeds a React frontend. It exposes three network endpoints: the HTTP API (with frontend), the OpAMP WebSocket server, and the browser WebSocket hub.

## Top-level layout

```mermaid
flowchart LR
    subgraph browser["Browser (React + Vite)"]
        ui[UI]
    end
    subgraph binary["otel-magnify binary"]
        api[REST API + WS hub<br/>chi router]
        opamp[OpAMP server<br/>opamp-go]
        alerts[Alert engine<br/>30s tick]
        store[(Store<br/>SQLite / Postgres)]
    end
    subgraph agents["Agents"]
        col[OTel Collectors]
        sdk[SDK agents]
    end

    ui <-->|REST + WS<br/>:8080| api
    col <-->|OpAMP WS<br/>:4320| opamp
    sdk <-->|OpAMP WS<br/>:4320| opamp
    api --> store
    opamp --> store
    alerts --> store
    alerts -->|events| api
    opamp -->|events| api
```

## Module layout

```
backend/
├── cmd/server/          # entrypoint, embeds frontend via embed.FS
├── cmd/sdkagent/        # SDK agent simulator (dev tool)
├── internal/
│   ├── api/             # chi router, REST handlers, WebSocket hub
│   ├── alerts/          # alert engine, webhook notifier
│   ├── auth/            # JWT HS256, middleware
│   ├── config/          # env-based configuration
│   ├── opamp/           # OpAMP server, agent registry, config push
│   └── store/           # SQLite/Postgres via goose migrations
└── pkg/models/          # shared structs
```

## Key design decisions

- **`pressly/goose` over `golang-migrate`** — better `modernc.org/sqlite` support (pure Go, no CGO required).
- **OpAMP server uses `Attach()`** to mount on the chi mux, not as a standalone server.
- **Agent type detection via `isCollectorName()`** — matches the `otelcol*` prefix patterns.
- **WebSocket auth via `?token=` query parameter** — browsers cannot set custom headers on WS handshakes.
- **Frontend served via `embed.FS` with SPA fallback** for the single-binary deployment model.
