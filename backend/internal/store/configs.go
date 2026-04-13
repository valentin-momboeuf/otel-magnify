package store

import (
	"database/sql"
	"fmt"
	"time"

	"otel-magnify/pkg/models"
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

func (d *DB) RecordAgentConfig(agentID, configID, status string) error {
	_, err := d.Exec(`
		INSERT INTO agent_configs (agent_id, config_id, applied_at, status)
		VALUES (?, ?, ?, ?)`,
		agentID, configID, time.Now().UTC(), status,
	)
	return err
}

func (d *DB) GetLatestPendingAgentConfig(agentID string) (*models.AgentConfig, error) {
	var ac models.AgentConfig
	err := d.QueryRow(`
		SELECT agent_id, config_id, applied_at, status
		FROM agent_configs WHERE agent_id = ? AND status = 'pending'
		ORDER BY applied_at DESC LIMIT 1`, agentID,
	).Scan(&ac.AgentID, &ac.ConfigID, &ac.AppliedAt, &ac.Status)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ac, nil
}

func (d *DB) GetAgentConfigHistory(agentID string) ([]models.AgentConfig, error) {
	rows, err := d.Query(`
		SELECT agent_id, config_id, applied_at, status
		FROM agent_configs WHERE agent_id = ? ORDER BY applied_at DESC`, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var history []models.AgentConfig
	for rows.Next() {
		var ac models.AgentConfig
		if err := rows.Scan(&ac.AgentID, &ac.ConfigID, &ac.AppliedAt, &ac.Status); err != nil {
			return nil, err
		}
		history = append(history, ac)
	}
	return history, rows.Err()
}
