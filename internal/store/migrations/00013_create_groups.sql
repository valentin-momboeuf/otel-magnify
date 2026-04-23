-- +goose Up
CREATE TABLE groups (
    id          TEXT PRIMARY KEY,
    name        TEXT UNIQUE NOT NULL,
    role        TEXT NOT NULL
                CHECK (role IN ('viewer','editor','administrator')),
    is_system   INTEGER NOT NULL DEFAULT 0,
    created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO groups (id, name, role, is_system) VALUES
    ('grp_system_viewer',        'viewer',        'viewer',        1),
    ('grp_system_editor',        'editor',        'editor',        1),
    ('grp_system_administrator', 'administrator', 'administrator', 1);

-- +goose Down
DROP TABLE IF EXISTS groups;
