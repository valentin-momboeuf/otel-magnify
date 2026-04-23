package store

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

// defaultPreferences mirrors the CHECK defaults on the user_preferences
// table. Returned when no row exists for the user.
func defaultPreferences(userID string) models.UserPreferences {
	return models.UserPreferences{
		UserID: userID, Theme: "system", Language: "en",
	}
}

// GetUserPreferences returns the persisted preferences or the defaults
// when the user has no row yet.
func (d *DB) GetUserPreferences(userID string) (models.UserPreferences, error) {
	var p models.UserPreferences
	err := d.QueryRow(`
		SELECT user_id, theme, language, updated_at
		FROM user_preferences WHERE user_id = ?`, userID,
	).Scan(&p.UserID, &p.Theme, &p.Language, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return defaultPreferences(userID), nil
	}
	if err != nil {
		return p, fmt.Errorf("get preferences %s: %w", userID, err)
	}
	return p, nil
}

// UpsertUserPreferences inserts or replaces the preferences row. CHECK
// constraints on the table enforce valid theme/language values; callers
// can rely on the returned error to surface invalid input.
func (d *DB) UpsertUserPreferences(p models.UserPreferences) error {
	_, err := d.Exec(`
		INSERT INTO user_preferences (user_id, theme, language, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET
			theme      = excluded.theme,
			language   = excluded.language,
			updated_at = excluded.updated_at`,
		p.UserID, p.Theme, p.Language, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("upsert preferences %s: %w", p.UserID, err)
	}
	return nil
}
