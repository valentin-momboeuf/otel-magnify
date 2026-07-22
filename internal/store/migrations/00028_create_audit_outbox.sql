-- +goose Up
-- +goose StatementBegin
CREATE TABLE audit_outbox (
    event_id TEXT PRIMARY KEY CHECK (length(trim(event_id)) BETWEEN 1 AND 128),
    occurred_at TIMESTAMPTZ NOT NULL,
    action TEXT NOT NULL CHECK (length(trim(action)) BETWEEN 1 AND 128),
    user_id TEXT NOT NULL DEFAULT '' CHECK (length(user_id) <= 256),
    email TEXT NOT NULL DEFAULT '' CHECK (length(email) <= 320),
    resource TEXT NOT NULL CHECK (length(trim(resource)) BETWEEN 1 AND 128),
    resource_id TEXT NOT NULL DEFAULT '' CHECK (length(resource_id) <= 256),
    detail TEXT NOT NULL DEFAULT '' CHECK (length(detail) <= 4096),
    attempt_count INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    next_attempt_at TIMESTAMPTZ NOT NULL,
    claim_token UUID,
    lease_until TIMESTAMPTZ,
    delivered_at TIMESTAMPTZ,
    CHECK ((claim_token IS NULL) = (lease_until IS NULL)),
    CHECK (delivered_at IS NULL OR (claim_token IS NULL AND lease_until IS NULL))
);

CREATE INDEX idx_audit_outbox_pending
    ON audit_outbox (next_attempt_at, occurred_at, event_id)
    WHERE delivered_at IS NULL;
-- +goose StatementEnd

-- +goose Down
DROP TABLE audit_outbox;
