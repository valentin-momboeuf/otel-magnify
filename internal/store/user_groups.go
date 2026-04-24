package store

import (
	"database/sql"
	"fmt"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

// AttachUserToGroupByName inserts a membership row, no-op if already present.
// Resolves the group by name so callers don't need to juggle system-group IDs.
func (d *DB) AttachUserToGroupByName(userID, groupName string) error {
	g, err := d.GetGroupByName(groupName)
	if err != nil {
		return err
	}
	_, err = d.Exec(`
		INSERT INTO user_groups (user_id, group_id)
		VALUES (?, ?)
		ON CONFLICT DO NOTHING`, userID, g.ID)
	if err != nil {
		return fmt.Errorf("attach user %s to %s: %w", userID, groupName, err)
	}
	return nil
}

// DetachUserFromGroup removes a user's membership in a named group.
// The call is idempotent against the membership: detaching a user
// who is not currently in the group returns nil.
//
// Unlike AttachUserToGroupByName, this method validates the user's
// existence up front (returns an error for unknown users) so SSO
// group-sync failures surface as caller bugs instead of silent no-ops.
// An unknown group name always returns an error.
func (d *DB) DetachUserFromGroup(userID, groupName string) error {
	var exists int
	if err := d.QueryRow(`SELECT 1 FROM users WHERE id = ?`, userID).Scan(&exists); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("detach user %s from %s: user not found: %w", userID, groupName, err)
		}
		return fmt.Errorf("detach user %s from %s: check user: %w", userID, groupName, err)
	}
	g, err := d.GetGroupByName(groupName)
	if err != nil {
		return err
	}
	if _, err := d.Exec(
		`DELETE FROM user_groups WHERE user_id = ? AND group_id = ?`,
		userID, g.ID,
	); err != nil {
		return fmt.Errorf("detach user %s from %s: %w", userID, groupName, err)
	}
	return nil
}

// GetUserGroups returns all groups the user belongs to. Empty slice if the
// user has no memberships or does not exist.
func (d *DB) GetUserGroups(userID string) ([]models.Group, error) {
	rows, err := d.Query(`
		SELECT g.id, g.name, g.role, g.is_system, g.created_at
		FROM groups g
		INNER JOIN user_groups ug ON ug.group_id = g.id
		WHERE ug.user_id = ?
		ORDER BY g.name`, userID)
	if err != nil {
		return nil, fmt.Errorf("list groups for %s: %w", userID, err)
	}
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
