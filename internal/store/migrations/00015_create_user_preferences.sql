-- +goose Up
CREATE TABLE user_preferences (
    user_id    TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    theme      TEXT NOT NULL DEFAULT 'system'
               CHECK (theme IN ('light','dark','system')),
    language   TEXT NOT NULL DEFAULT 'en'
               CHECK (language IN ('en','fr')),
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- +goose Down
DROP TABLE IF EXISTS user_preferences;
