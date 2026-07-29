-- +goose Up
-- +goose StatementBegin
CREATE TABLE opamp_tokens (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL CHECK (length(trim(name)) BETWEEN 1 AND 128),
    description TEXT NOT NULL DEFAULT '' CHECK (length(description) <= 512),
    team TEXT NOT NULL DEFAULT '' CHECK (length(team) <= 128),
    environment TEXT NOT NULL DEFAULT '' CHECK (length(environment) <= 128),
    secret_hash BYTEA NOT NULL CHECK (length(secret_hash) = 32),
    created_at TIMESTAMPTZ NOT NULL,
    created_by TEXT NOT NULL CHECK (trim(created_by) <> ''),
    expires_at TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    revoked_by TEXT,
    CHECK (expires_at IS NULL OR expires_at > created_at),
    CHECK (last_used_at IS NULL OR last_used_at >= created_at),
    CHECK (revoked_at IS NULL OR revoked_at >= created_at),
    CHECK (
        (revoked_at IS NULL AND revoked_by IS NULL)
        OR (
            revoked_at IS NOT NULL
            AND revoked_by IS NOT NULL
            AND trim(revoked_by) <> ''
        )
    )
);

CREATE INDEX idx_opamp_tokens_created
    ON opamp_tokens (created_at DESC, id);
CREATE INDEX idx_opamp_tokens_active_expiry
    ON opamp_tokens (expires_at)
    WHERE revoked_at IS NULL;
-- +goose StatementEnd

-- +goose Down
DROP TABLE opamp_tokens;
