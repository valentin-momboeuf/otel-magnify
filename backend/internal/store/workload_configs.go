package store

import (
	"database/sql"
	"time"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

func (d *DB) RecordWorkloadConfig(wc models.WorkloadConfig) error {
	t := wc.AppliedAt
	if t.IsZero() {
		t = time.Now().UTC()
	}
	_, err := d.Exec(`
		INSERT INTO workload_configs (workload_id, config_id, applied_at, status, error_message, pushed_by)
		VALUES (?, ?, ?, ?, ?, ?)`,
		wc.WorkloadID, wc.ConfigID, t, wc.Status, nullIfEmpty(wc.ErrorMessage), nullIfEmpty(wc.PushedBy),
	)
	return err
}

func (d *DB) UpdateWorkloadConfigStatus(workloadID, configID, status, errorMessage string) error {
	_, err := d.Exec(`
		UPDATE workload_configs SET status = ?, error_message = ?
		WHERE workload_id = ? AND config_id = ?
		  AND applied_at = (
		    SELECT MAX(applied_at) FROM workload_configs WHERE workload_id = ? AND config_id = ?
		  )`,
		status, nullIfEmpty(errorMessage), workloadID, configID, workloadID, configID,
	)
	return err
}

func (d *DB) GetLatestPendingWorkloadConfig(workloadID string) (*models.WorkloadConfig, error) {
	var wc models.WorkloadConfig
	err := d.QueryRow(`
		SELECT workload_id, config_id, applied_at, status,
		       COALESCE(error_message, ''), COALESCE(pushed_by, '')
		FROM workload_configs WHERE workload_id = ? AND status = 'pending'
		ORDER BY applied_at DESC LIMIT 1`, workloadID,
	).Scan(&wc.WorkloadID, &wc.ConfigID, &wc.AppliedAt, &wc.Status, &wc.ErrorMessage, &wc.PushedBy)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &wc, nil
}

func (d *DB) GetWorkloadConfigHistory(workloadID string) ([]models.WorkloadConfig, error) {
	rows, err := d.Query(`
		SELECT wc.workload_id, wc.config_id, wc.applied_at, wc.status,
		       COALESCE(wc.error_message, ''), COALESCE(wc.pushed_by, ''),
		       COALESCE(c.content, '')
		FROM workload_configs wc
		LEFT JOIN configs c ON c.id = wc.config_id
		WHERE wc.workload_id = ?
		ORDER BY wc.applied_at DESC`, workloadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var history []models.WorkloadConfig
	for rows.Next() {
		var wc models.WorkloadConfig
		if err := rows.Scan(&wc.WorkloadID, &wc.ConfigID, &wc.AppliedAt, &wc.Status,
			&wc.ErrorMessage, &wc.PushedBy, &wc.Content); err != nil {
			return nil, err
		}
		history = append(history, wc)
	}
	return history, rows.Err()
}

func (d *DB) GetLastAppliedWorkloadConfig(workloadID string) (*models.WorkloadConfig, error) {
	var wc models.WorkloadConfig
	err := d.QueryRow(`
		SELECT wc.workload_id, wc.config_id, wc.applied_at, wc.status,
		       COALESCE(wc.error_message, ''), COALESCE(wc.pushed_by, ''),
		       COALESCE(c.content, '')
		FROM workload_configs wc
		LEFT JOIN configs c ON c.id = wc.config_id
		WHERE wc.workload_id = ? AND wc.status = 'applied'
		ORDER BY wc.applied_at DESC LIMIT 1`, workloadID,
	).Scan(&wc.WorkloadID, &wc.ConfigID, &wc.AppliedAt, &wc.Status,
		&wc.ErrorMessage, &wc.PushedBy, &wc.Content)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &wc, nil
}
