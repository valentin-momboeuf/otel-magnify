-- +goose Up
-- +goose StatementBegin
CREATE TABLE user_groups (
    user_id  TEXT NOT NULL REFERENCES users(id)  ON DELETE CASCADE,
    group_id TEXT NOT NULL REFERENCES groups(id) ON DELETE RESTRICT,
    PRIMARY KEY (user_id, group_id)
);
-- +goose StatementEnd

-- Data migration : chaque user existant rejoint le groupe système correspondant.
-- +goose StatementBegin
INSERT INTO user_groups (user_id, group_id)
SELECT
    u.id,
    'grp_system_' || CASE u.role
        WHEN 'admin' THEN 'administrator'
        ELSE u.role
    END
FROM users u;
-- +goose StatementEnd

-- SQLite table-rebuild pour dropper users.role (DROP COLUMN n'existe pas en
-- SQLite < 3.35 et modernc.org/sqlite ne l'expose pas de façon fiable).
-- foreign_keys est déjà OFF au niveau connexion (cf. db.go Migrate).
-- +goose StatementBegin
CREATE TABLE users_new (
    id            TEXT PRIMARY KEY,
    email         TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    tenant_id     TEXT
);
-- +goose StatementEnd

-- +goose StatementBegin
INSERT INTO users_new (id, email, password_hash, tenant_id)
SELECT id, email, password_hash, tenant_id FROM users;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE users;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE users_new RENAME TO users;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT 'viewer';
-- +goose StatementEnd

-- +goose StatementBegin
UPDATE users SET role = 'admin'
WHERE id IN (
    SELECT user_id FROM user_groups WHERE group_id = 'grp_system_administrator'
);
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE IF EXISTS user_groups;
-- +goose StatementEnd
