package store

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/magnify-labs/otel-magnify/pkg/ext"
	"github.com/magnify-labs/otel-magnify/pkg/models"
)

// CreateUser inserts a new user row.
func (d *DB) CreateUser(u models.User) error {
	_, err := d.Exec(`
		INSERT INTO users (id, email, password_hash, tenant_id)
		VALUES (?, ?, ?, ?)`,
		u.ID, u.Email, u.PasswordHash, u.TenantID,
	)
	return err
}

// GetUserByEmail returns the user with the given email, wrapping ext.ErrUserNotFound on miss.
func (d *DB) GetUserByEmail(email string) (models.User, error) {
	var u models.User
	err := d.QueryRow(`
		SELECT id, email, password_hash, tenant_id
		FROM users WHERE email = ?`, email,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.TenantID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return u, fmt.Errorf("get user by email %s: %w", email, ext.ErrUserNotFound)
		}
		return u, fmt.Errorf("get user by email %s: %w", email, err)
	}
	return u, nil
}

// UpdateUser overwrites email, password_hash, and tenant_id of the row matching u.ID; returns sql.ErrNoRows when no row matches.
func (d *DB) UpdateUser(u models.User) error {
	res, err := d.Exec(`
		UPDATE users
		SET email = ?, password_hash = ?, tenant_id = ?
		WHERE id = ?`,
		u.Email, u.PasswordHash, u.TenantID, u.ID,
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
