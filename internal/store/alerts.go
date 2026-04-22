package store

import (
	"database/sql"
	"time"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

func (d *DB) CreateAlert(a models.Alert) error {
	_, err := d.Exec(`
		INSERT INTO alerts (id, workload_id, rule, severity, message, fired_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		a.ID, a.WorkloadID, a.Rule, a.Severity, a.Message, a.FiredAt.UTC(),
	)
	return err
}

func (d *DB) ResolveAlert(id string) error {
	res, err := d.Exec(`UPDATE alerts SET resolved_at = ? WHERE id = ?`, time.Now().UTC(), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (d *DB) ListAlerts(includeResolved bool) ([]models.Alert, error) {
	query := `SELECT id, workload_id, rule, severity, message, fired_at, resolved_at FROM alerts`
	if !includeResolved {
		query += ` WHERE resolved_at IS NULL`
	}
	query += ` ORDER BY fired_at DESC`

	rows, err := d.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var alerts []models.Alert
	for rows.Next() {
		var a models.Alert
		if err := rows.Scan(&a.ID, &a.WorkloadID, &a.Rule, &a.Severity, &a.Message, &a.FiredAt, &a.ResolvedAt); err != nil {
			return nil, err
		}
		alerts = append(alerts, a)
	}
	return alerts, rows.Err()
}

func (d *DB) GetUnresolvedAlertByWorkloadAndRule(workloadID, rule string) (*models.Alert, error) {
	var a models.Alert
	err := d.QueryRow(`
		SELECT id, workload_id, rule, severity, message, fired_at, resolved_at
		FROM alerts WHERE workload_id = ? AND rule = ? AND resolved_at IS NULL
		LIMIT 1`, workloadID, rule,
	).Scan(&a.ID, &a.WorkloadID, &a.Rule, &a.Severity, &a.Message, &a.FiredAt, &a.ResolvedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}
