package store

import (
	"fmt"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

func (d *DB) CreateConfig(c models.Config) error {
	_, err := d.Exec(`
		INSERT INTO configs (id, name, content, created_at, created_by)
		VALUES (?, ?, ?, ?, ?)`,
		c.ID, c.Name, c.Content, c.CreatedAt.UTC(), c.CreatedBy,
	)
	return err
}

func (d *DB) GetConfig(id string) (models.Config, error) {
	var c models.Config
	err := d.QueryRow(`SELECT id, name, content, created_at, created_by FROM configs WHERE id = ?`, id).
		Scan(&c.ID, &c.Name, &c.Content, &c.CreatedAt, &c.CreatedBy)
	if err != nil {
		return c, fmt.Errorf("get config %s: %w", id, err)
	}
	return c, nil
}

func (d *DB) ListConfigs() ([]models.Config, error) {
	rows, err := d.Query(`SELECT id, name, content, created_at, created_by FROM configs ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	//nolint:errcheck // deferred cleanup; rows fully iterated below
	defer rows.Close()

	var configs []models.Config
	for rows.Next() {
		var c models.Config
		if err := rows.Scan(&c.ID, &c.Name, &c.Content, &c.CreatedAt, &c.CreatedBy); err != nil {
			return nil, err
		}
		configs = append(configs, c)
	}
	return configs, rows.Err()
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
