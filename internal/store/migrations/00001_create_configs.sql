-- +goose Up
CREATE TABLE configs (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    content    TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT NOT NULL DEFAULT ''
);

-- +goose Down
DROP TABLE IF EXISTS configs;
