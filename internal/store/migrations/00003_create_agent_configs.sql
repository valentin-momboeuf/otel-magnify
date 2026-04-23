-- +goose Up
CREATE TABLE agent_configs (
    agent_id   TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    config_id  TEXT NOT NULL REFERENCES configs(id),
    applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    status     TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'applied', 'failed')),
    PRIMARY KEY (agent_id, config_id, applied_at)
);

-- +goose Down
DROP TABLE IF EXISTS agent_configs;
