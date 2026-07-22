package store

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestMigration00027CreatesOpAMPTokenSchema(t *testing.T) {
	db := newTestDB(t)

	var idType string
	if err := db.QueryRow(`
		SELECT data_type
		FROM information_schema.columns
		WHERE table_schema = current_schema()
		  AND table_name = 'opamp_tokens'
		  AND column_name = 'id'`).Scan(&idType); err != nil {
		t.Fatalf("query opamp_tokens.id type: %v", err)
	}
	if idType != "uuid" {
		t.Fatalf("opamp_tokens.id type = %q, want uuid", idType)
	}

	expectedIndexes := map[string]string{
		"idx_opamp_tokens_created":       "(created_at DESC, id)",
		"idx_opamp_tokens_active_expiry": "(expires_at) WHERE (revoked_at IS NULL)",
	}
	for name, expectedFragment := range expectedIndexes {
		var definition string
		if err := db.QueryRow(`
			SELECT indexdef
			FROM pg_indexes
			WHERE schemaname = current_schema() AND indexname = ?`, name,
		).Scan(&definition); err != nil {
			t.Fatalf("query index %s: %v", name, err)
		}
		if !strings.Contains(definition, expectedFragment) {
			t.Fatalf("index %s definition = %q, want fragment %q", name, definition, expectedFragment)
		}
	}

	assertGooseVersion(t, db, 27)
}

func TestMigration00027EnforcesOpAMPTokenConstraints(t *testing.T) {
	db := newTestDB(t)
	createdAt := time.Date(2026, time.July, 22, 8, 0, 0, 0, time.UTC)

	type fixture struct {
		id          string
		name        string
		description string
		team        string
		environment string
		secretHash  []byte
		createdBy   string
		expiresAt   *time.Time
		lastUsedAt  *time.Time
		revokedAt   *time.Time
		revokedBy   *string
	}

	validFixture := func(index int) fixture {
		return fixture{
			id:          fmt.Sprintf("00000000-0000-4000-8000-%012d", index),
			name:        "production",
			description: "production collectors",
			team:        "platform",
			environment: "production",
			secretHash:  bytes.Repeat([]byte{byte(index)}, 32),
			createdBy:   "operator@example.com",
		}
	}

	beforeCreation := createdAt.Add(-time.Second)
	atCreation := createdAt
	afterCreation := createdAt.Add(time.Second)
	revoker := "security@example.com"
	blankRevoker := "   "

	tests := []struct {
		name   string
		mutate func(*fixture)
	}{
		{name: "non_uuid_id", mutate: func(row *fixture) { row.id = "not-a-uuid" }},
		{name: "blank_name", mutate: func(row *fixture) { row.name = "   " }},
		{name: "name_too_long", mutate: func(row *fixture) { row.name = strings.Repeat("n", 129) }},
		{name: "description_too_long", mutate: func(row *fixture) { row.description = strings.Repeat("d", 513) }},
		{name: "team_too_long", mutate: func(row *fixture) { row.team = strings.Repeat("t", 129) }},
		{name: "environment_too_long", mutate: func(row *fixture) { row.environment = strings.Repeat("e", 129) }},
		{name: "short_secret_hash", mutate: func(row *fixture) { row.secretHash = bytes.Repeat([]byte{1}, 31) }},
		{name: "long_secret_hash", mutate: func(row *fixture) { row.secretHash = bytes.Repeat([]byte{1}, 33) }},
		{name: "blank_created_by", mutate: func(row *fixture) { row.createdBy = "   " }},
		{name: "expiration_at_creation", mutate: func(row *fixture) { row.expiresAt = &atCreation }},
		{name: "last_use_before_creation", mutate: func(row *fixture) { row.lastUsedAt = &beforeCreation }},
		{name: "revocation_before_creation", mutate: func(row *fixture) {
			row.revokedAt = &beforeCreation
			row.revokedBy = &revoker
		}},
		{name: "revocation_without_actor", mutate: func(row *fixture) { row.revokedAt = &afterCreation }},
		{name: "actor_without_revocation", mutate: func(row *fixture) { row.revokedBy = &revoker }},
		{name: "blank_revocation_actor", mutate: func(row *fixture) {
			row.revokedAt = &afterCreation
			row.revokedBy = &blankRevoker
		}},
	}

	for index, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := validFixture(index + 1)
			tt.mutate(&row)
			_, err := db.Exec(`
				INSERT INTO opamp_tokens (
					id, name, description, team, environment, secret_hash,
					created_at, created_by, expires_at, last_used_at, revoked_at, revoked_by
				) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				row.id, row.name, row.description, row.team, row.environment, row.secretHash,
				createdAt, row.createdBy, row.expiresAt, row.lastUsedAt, row.revokedAt, row.revokedBy,
			)
			if err == nil {
				t.Fatal("INSERT error = nil, want constraint violation")
			}
		})
	}

	valid := validFixture(len(tests) + 1)
	valid.name = strings.Repeat("n", 128)
	valid.description = strings.Repeat("d", 512)
	valid.team = strings.Repeat("t", 128)
	valid.environment = strings.Repeat("e", 128)
	valid.expiresAt = &afterCreation
	valid.lastUsedAt = &atCreation
	valid.revokedAt = &atCreation
	valid.revokedBy = &revoker
	if _, err := db.Exec(`
		INSERT INTO opamp_tokens (
			id, name, description, team, environment, secret_hash,
			created_at, created_by, expires_at, last_used_at, revoked_at, revoked_by
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		valid.id, valid.name, valid.description, valid.team, valid.environment, valid.secretHash,
		createdAt, valid.createdBy, valid.expiresAt, valid.lastUsedAt, valid.revokedAt, valid.revokedBy,
	); err != nil {
		t.Fatalf("insert boundary-valid token: %v", err)
	}
}
