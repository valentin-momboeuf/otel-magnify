-- +goose Up
CREATE TABLE users (
    id            TEXT PRIMARY KEY,
    email         TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    role          TEXT NOT NULL DEFAULT 'viewer' CHECK (role IN ('admin', 'viewer')),
    tenant_id     TEXT
);

-- +goose Down
DROP TABLE IF EXISTS users;
