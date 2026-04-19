package store

import (
	"database/sql"
	"fmt"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

func (d *DB) CreateUser(u models.User) error {
	_, err := d.Exec(`
		INSERT INTO users (id, email, password_hash, role, tenant_id)
		VALUES (?, ?, ?, ?, ?)`,
		u.ID, u.Email, u.PasswordHash, u.Role, u.TenantID,
	)
	return err
}

func (d *DB) GetUserByEmail(email string) (models.User, error) {
	var u models.User
	err := d.QueryRow(`
		SELECT id, email, password_hash, role, tenant_id
		FROM users WHERE email = ?`, email,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.TenantID)
	if err != nil {
		return u, fmt.Errorf("get user by email %s: %w", email, err)
	}
	return u, nil
}

func (d *DB) UpdateUser(u models.User) error {
	res, err := d.Exec(`
		UPDATE users
		SET email = ?, password_hash = ?, role = ?, tenant_id = ?
		WHERE id = ?`,
		u.Email, u.PasswordHash, u.Role, u.TenantID, u.ID,
	)
	if err != nil {
		return fmt.Errorf("update user %s: %w", u.ID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update user %s (rows affected): %w", u.ID, err)
	}
	if n == 0 {
		return fmt.Errorf("update user %s: %w", u.ID, sql.ErrNoRows)
	}
	return nil
}
