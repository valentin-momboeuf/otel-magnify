package store

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/magnify-labs/otel-magnify/pkg/ext"
	"github.com/magnify-labs/otel-magnify/pkg/models"
)

const opAMPTokenActivityWindow = 30 * time.Second

// ListOpAMPTokens returns public token metadata ordered newest first.
func (d *DB) ListOpAMPTokens(ctx context.Context, now time.Time) ([]models.OpAMPToken, error) {
	rows, err := d.DB.QueryContext(ctx, `
		SELECT id, name, description, team, environment, created_at, created_by,
		       expires_at, last_used_at, revoked_at, revoked_by,
		       CASE
		           WHEN revoked_at IS NOT NULL THEN $1
		           WHEN expires_at IS NOT NULL AND expires_at <= $2 THEN $3
		           ELSE $4
		       END
		FROM opamp_tokens
		ORDER BY created_at DESC, id`,
		models.OpAMPTokenRevoked, now.UTC(), models.OpAMPTokenExpired, models.OpAMPTokenActive,
	)
	if err != nil {
		return nil, fmt.Errorf("list OpAMP tokens: %w", err)
	}
	defer rows.Close() //nolint:errcheck // rows are fully iterated below

	var tokens []models.OpAMPToken
	for rows.Next() {
		var token models.OpAMPToken
		var expiresAt sql.NullTime
		var lastUsedAt sql.NullTime
		var revokedAt sql.NullTime
		var revokedBy sql.NullString
		if err := rows.Scan(
			&token.ID, &token.Name, &token.Description, &token.Team, &token.Environment,
			&token.CreatedAt, &token.CreatedBy, &expiresAt, &lastUsedAt, &revokedAt,
			&revokedBy, &token.Status,
		); err != nil {
			return nil, fmt.Errorf("scan OpAMP token: %w", err)
		}
		token.CreatedAt = token.CreatedAt.UTC()
		token.ExpiresAt = nullableTimeUTC(expiresAt)
		token.LastUsedAt = nullableTimeUTC(lastUsedAt)
		token.RevokedAt = nullableTimeUTC(revokedAt)
		if revokedBy.Valid {
			token.RevokedBy = revokedBy.String
		}
		tokens = append(tokens, token)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate OpAMP tokens: %w", err)
	}
	return tokens, nil
}

// ValidateOpAMPToken verifies a managed token without recording activity.
func (d *DB) ValidateOpAMPToken(ctx context.Context, id string, presentedHash [32]byte, now time.Time) (models.OpAMPTokenPrincipal, error) {
	if !isCanonicalOpAMPTokenID(id) {
		return models.OpAMPTokenPrincipal{}, ext.ErrInvalidOpAMPToken
	}

	comparisonDigest := dummyOpAMPTokenDigest()
	var storedDigest []byte
	var expiresAt sql.NullTime
	var revokedAt sql.NullTime
	err := d.DB.QueryRowContext(ctx, `
		SELECT secret_hash, expires_at, revoked_at
		FROM opamp_tokens
		WHERE id = $1`, id,
	).Scan(&storedDigest, &expiresAt, &revokedAt)
	found := err == nil
	switch {
	case found && len(storedDigest) != len(comparisonDigest):
		return models.OpAMPTokenPrincipal{}, fmt.Errorf("validate OpAMP token: stored digest has invalid length")
	case found:
		copy(comparisonDigest[:], storedDigest)
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return models.OpAMPTokenPrincipal{}, fmt.Errorf("validate OpAMP token: %w", err)
	}

	hashMatches := subtle.ConstantTimeCompare(presentedHash[:], comparisonDigest[:]) == 1
	if !found || !hashMatches || revokedAt.Valid || (expiresAt.Valid && !expiresAt.Time.After(now)) {
		return models.OpAMPTokenPrincipal{}, ext.ErrInvalidOpAMPToken
	}

	return models.OpAMPTokenPrincipal{
		ID:        id,
		ExpiresAt: nullableTimeUTC(expiresAt),
	}, nil
}

// MarkOpAMPTokenUsed conditionally records token activity once per activity window.
func (d *DB) MarkOpAMPTokenUsed(ctx context.Context, id string, now time.Time) error {
	if !isCanonicalOpAMPTokenID(id) {
		return ext.ErrInvalidOpAMPToken
	}
	now = now.UTC()

	tx, err := d.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("mark OpAMP token used: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var lastUsedAt sql.NullTime
	err = tx.QueryRowContext(ctx, `
		SELECT last_used_at
		FROM opamp_tokens
		WHERE id = $1
		  AND revoked_at IS NULL
		  AND (expires_at IS NULL OR expires_at > $2)
		FOR UPDATE`, id, now,
	).Scan(&lastUsedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ext.ErrInvalidOpAMPToken
	}
	if err != nil {
		return fmt.Errorf("mark OpAMP token used: lock token: %w", err)
	}

	if !lastUsedAt.Valid || !lastUsedAt.Time.After(now.Add(-opAMPTokenActivityWindow)) {
		if _, err := tx.ExecContext(ctx, `
			UPDATE opamp_tokens
			SET last_used_at = $1
			WHERE id = $2`, now, id,
		); err != nil {
			return fmt.Errorf("mark OpAMP token used: update activity: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("mark OpAMP token used: commit: %w", err)
	}
	return nil
}

func isCanonicalOpAMPTokenID(id string) bool {
	parsedID, err := uuid.Parse(id)
	return err == nil && id == parsedID.String()
}

func dummyOpAMPTokenDigest() [32]byte {
	return [32]byte{
		0x9f, 0xd4, 0x17, 0xe6, 0x4e, 0x98, 0xa8, 0xce,
		0x90, 0x2d, 0x43, 0x60, 0x78, 0x25, 0xf1, 0xe7,
		0x3a, 0x22, 0xb4, 0x1b, 0xa5, 0x12, 0xeb, 0x2c,
		0x81, 0x21, 0xee, 0x47, 0x69, 0x2a, 0x34, 0xf8,
	}
}

func nullableTimeUTC(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	utc := value.Time.UTC()
	return &utc
}
