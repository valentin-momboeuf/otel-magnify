package store

import (
	"fmt"

	"otel-magnify/pkg/models"
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
