-- +goose Up
ALTER TABLE agents ADD COLUMN accepts_remote_config INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE agents DROP COLUMN accepts_remote_config;
