-- +goose Up
CREATE TABLE agents (
    id               TEXT PRIMARY KEY,
    display_name     TEXT NOT NULL DEFAULT '',
    type             TEXT NOT NULL CHECK (type IN ('collector', 'sdk')),
    version          TEXT NOT NULL DEFAULT '',
    status           TEXT NOT NULL DEFAULT 'disconnected' CHECK (status IN ('connected', 'disconnected', 'degraded')),
    last_seen_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    labels           TEXT NOT NULL DEFAULT '{}',
    active_config_id TEXT REFERENCES configs(id)
);

-- +goose Down
DROP TABLE IF EXISTS agents;
