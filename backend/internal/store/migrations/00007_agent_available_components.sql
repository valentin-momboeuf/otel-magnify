-- +goose Up
ALTER TABLE agents ADD COLUMN available_components TEXT;

-- +goose Down
ALTER TABLE agents DROP COLUMN available_components;
