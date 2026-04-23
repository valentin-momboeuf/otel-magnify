-- +goose Up
-- +goose StatementBegin
CREATE TABLE agent_configs_new (
    agent_id      TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    config_id     TEXT NOT NULL REFERENCES configs(id),
    applied_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    status        TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'applying', 'applied', 'failed')),
    error_message TEXT,
    pushed_by     TEXT,
    PRIMARY KEY (agent_id, config_id, applied_at)
);

INSERT INTO agent_configs_new (agent_id, config_id, applied_at, status, error_message, pushed_by)
SELECT agent_id, config_id, applied_at, status, error_message, pushed_by FROM agent_configs;

DROP TABLE agent_configs;
ALTER TABLE agent_configs_new RENAME TO agent_configs;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE TABLE agent_configs_old (
    agent_id      TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    config_id     TEXT NOT NULL REFERENCES configs(id),
    applied_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    status        TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'applied', 'failed')),
    error_message TEXT,
    pushed_by     TEXT,
    PRIMARY KEY (agent_id, config_id, applied_at)
);

INSERT INTO agent_configs_old (agent_id, config_id, applied_at, status, error_message, pushed_by)
SELECT agent_id, config_id, applied_at,
       CASE WHEN status = 'applying' THEN 'pending' ELSE status END,
       error_message, pushed_by FROM agent_configs;

DROP TABLE agent_configs;
ALTER TABLE agent_configs_old RENAME TO agent_configs;
-- +goose StatementEnd
