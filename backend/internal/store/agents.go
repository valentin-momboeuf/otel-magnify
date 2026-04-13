package store

import (
	"database/sql"
	"fmt"
	"time"

	"otel-magnify/pkg/models"
)

func (d *DB) UpsertAgent(a models.Agent) error {
	labelsJSON, err := a.Labels.Value()
	if err != nil {
		return fmt.Errorf("marshal labels: %w", err)
	}
	_, err = d.Exec(`
		INSERT INTO agents (id, display_name, type, version, status, last_seen_at, labels, active_config_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			display_name = excluded.display_name,
			type = excluded.type,
			version = excluded.version,
			status = excluded.status,
			last_seen_at = excluded.last_seen_at,
			labels = excluded.labels,
			active_config_id = excluded.active_config_id`,
		a.ID, a.DisplayName, a.Type, a.Version, a.Status, a.LastSeenAt.UTC(), labelsJSON, a.ActiveConfigID,
	)
	return err
}

func (d *DB) GetAgent(id string) (models.Agent, error) {
	var a models.Agent
	var labelsJSON string
	err := d.QueryRow(`
		SELECT id, display_name, type, version, status, last_seen_at, labels, active_config_id
		FROM agents WHERE id = ?`, id,
	).Scan(&a.ID, &a.DisplayName, &a.Type, &a.Version, &a.Status, &a.LastSeenAt, &labelsJSON, &a.ActiveConfigID)
	if err != nil {
		return a, fmt.Errorf("get agent %s: %w", id, err)
	}
	if err := a.Labels.Scan(labelsJSON); err != nil {
		return a, fmt.Errorf("scan labels: %w", err)
	}
	return a, nil
}

func (d *DB) ListAgents() ([]models.Agent, error) {
	rows, err := d.Query(`
		SELECT id, display_name, type, version, status, last_seen_at, labels, active_config_id
		FROM agents ORDER BY display_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var agents []models.Agent
	for rows.Next() {
		var a models.Agent
		var labelsJSON string
		if err := rows.Scan(&a.ID, &a.DisplayName, &a.Type, &a.Version, &a.Status, &a.LastSeenAt, &labelsJSON, &a.ActiveConfigID); err != nil {
			return nil, err
		}
		if err := a.Labels.Scan(labelsJSON); err != nil {
			return nil, err
		}
		agents = append(agents, a)
	}
	return agents, rows.Err()
}

func (d *DB) UpdateAgentStatus(id, status string) error {
	res, err := d.Exec(`UPDATE agents SET status = ?, last_seen_at = ? WHERE id = ?`,
		status, time.Now().UTC(), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
