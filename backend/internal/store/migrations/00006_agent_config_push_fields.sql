-- +goose Up
ALTER TABLE agent_configs ADD COLUMN error_message TEXT;
ALTER TABLE agent_configs ADD COLUMN pushed_by TEXT;
ALTER TABLE agents ADD COLUMN remote_config_status TEXT;

-- +goose Down
ALTER TABLE agents DROP COLUMN remote_config_status;
ALTER TABLE agent_configs DROP COLUMN pushed_by;
ALTER TABLE agent_configs DROP COLUMN error_message;
