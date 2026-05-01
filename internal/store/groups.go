package store

import (
	"database/sql"
	"fmt"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

// ListSystemGroups returns the three groups seeded at migration time.
func (d *DB) ListSystemGroups() ([]models.Group, error) {
	rows, err := d.Query(`
		SELECT id, name, role, is_system, created_at
		FROM groups WHERE is_system = 1
		ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list system groups: %w", err)
	}
	//nolint:errcheck // deferred cleanup; rows fully iterated below
	defer rows.Close()

	var out []models.Group
	for rows.Next() {
		var g models.Group
		var isSys int
		if err := rows.Scan(&g.ID, &g.Name, &g.Role, &isSys, &g.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan group: %w", err)
		}
		g.IsSystem = isSys == 1
		out = append(out, g)
	}
	return out, rows.Err()
}

// GetGroupByName loads a single group by its unique name.
func (d *DB) GetGroupByName(name string) (models.Group, error) {
	var g models.Group
	var isSys int
	err := d.QueryRow(`
		SELECT id, name, role, is_system, created_at
		FROM groups WHERE name = ?`, name,
	).Scan(&g.ID, &g.Name, &g.Role, &isSys, &g.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return g, fmt.Errorf("group %q: %w", name, sql.ErrNoRows)
		}
		return g, fmt.Errorf("get group %q: %w", name, err)
	}
	g.IsSystem = isSys == 1
	return g, nil
}
