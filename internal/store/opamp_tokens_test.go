package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/magnify-labs/otel-magnify/internal/testdb"
	"github.com/magnify-labs/otel-magnify/pkg/ext"
	"github.com/magnify-labs/otel-magnify/pkg/models"
)

func TestOpAMPTokenListReturnsSortedPublicMetadataAndStatus(t *testing.T) {
	db := newTestDB(t)
	now := time.Date(2026, time.July, 22, 10, 0, 0, 0, time.UTC)
	older := now.Add(-2 * time.Hour)
	newer := now.Add(-time.Hour)
	expiredAt := now
	expiredLastUsedAt := now.Add(-30 * time.Minute)
	revokedAt := now.Add(-30 * time.Minute)
	revokedLastUsedAt := now.Add(-90 * time.Minute)
	activeLastUsedAt := now.Add(-15 * time.Minute)
	revokedBy := "security@example.com"

	activeHash := sha256.Sum256([]byte("active-secret"))
	expiredHash := sha256.Sum256([]byte("expired-secret"))
	revokedHash := sha256.Sum256([]byte("revoked-secret"))
	seedOpAMPToken(t, db, opAMPTokenFixture{
		id: "00000000-0000-4000-8000-000000000003", name: "active", description: "active token",
		team: "platform", environment: "production", secretHash: activeHash, createdAt: newer,
		createdBy: "active-owner@example.com", lastUsedAt: &activeLastUsedAt,
	})
	seedOpAMPToken(t, db, opAMPTokenFixture{
		id: "00000000-0000-4000-8000-000000000002", name: "expired", description: "expired integration token",
		team: "observability", environment: "staging", secretHash: expiredHash, createdAt: newer,
		createdBy: "expired-owner@example.com", expiresAt: &expiredAt, lastUsedAt: &expiredLastUsedAt,
	})
	seedOpAMPToken(t, db, opAMPTokenFixture{
		id: "00000000-0000-4000-8000-000000000001", name: "revoked", description: "revoked emergency token",
		team: "security", environment: "sandbox", secretHash: revokedHash, createdAt: older,
		createdBy: "revoked-owner@example.com", expiresAt: &expiredAt, lastUsedAt: &revokedLastUsedAt,
		revokedAt: &revokedAt, revokedBy: &revokedBy,
	})

	tokens, err := db.ListOpAMPTokens(context.Background(), now)
	if err != nil {
		t.Fatalf("ListOpAMPTokens() error = %v", err)
	}
	if len(tokens) != 3 {
		t.Fatalf("ListOpAMPTokens() length = %d, want 3", len(tokens))
	}

	want := []models.OpAMPToken{
		{
			ID: "00000000-0000-4000-8000-000000000002", Name: "expired", Description: "expired integration token",
			Team: "observability", Environment: "staging", CreatedAt: newer, CreatedBy: "expired-owner@example.com",
			ExpiresAt: &expiredAt, LastUsedAt: &expiredLastUsedAt, Status: models.OpAMPTokenExpired,
		},
		{
			ID: "00000000-0000-4000-8000-000000000003", Name: "active", Description: "active token",
			Team: "platform", Environment: "production", CreatedAt: newer, CreatedBy: "active-owner@example.com",
			LastUsedAt: &activeLastUsedAt, Status: models.OpAMPTokenActive,
		},
		{
			ID: "00000000-0000-4000-8000-000000000001", Name: "revoked", Description: "revoked emergency token",
			Team: "security", Environment: "sandbox", CreatedAt: older, CreatedBy: "revoked-owner@example.com",
			ExpiresAt: &expiredAt, LastUsedAt: &revokedLastUsedAt, RevokedAt: &revokedAt,
			RevokedBy: revokedBy, Status: models.OpAMPTokenRevoked,
		},
	}
	for index := range tokens {
		assertOpAMPTokenEqual(t, index, tokens[index], want[index])
	}

	encoded, err := json.Marshal(tokens)
	if err != nil {
		t.Fatalf("marshal listed tokens: %v", err)
	}
	encodedText := string(encoded)
	for _, digest := range [][32]byte{activeHash, expiredHash, revokedHash} {
		if strings.Contains(encodedText, "secret_hash") || strings.Contains(encodedText, hex.EncodeToString(digest[:])) {
			t.Fatalf("listed token JSON discloses a secret hash: %s", encodedText)
		}
	}
}

func assertOpAMPTokenEqual(t *testing.T, index int, got, want models.OpAMPToken) {
	t.Helper()
	if got.ID != want.ID {
		t.Errorf("token[%d].ID = %q, want %q", index, got.ID, want.ID)
	}
	if got.Name != want.Name {
		t.Errorf("token[%d].Name = %q, want %q", index, got.Name, want.Name)
	}
	if got.Description != want.Description {
		t.Errorf("token[%d].Description = %q, want %q", index, got.Description, want.Description)
	}
	if got.Team != want.Team {
		t.Errorf("token[%d].Team = %q, want %q", index, got.Team, want.Team)
	}
	if got.Environment != want.Environment {
		t.Errorf("token[%d].Environment = %q, want %q", index, got.Environment, want.Environment)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) {
		t.Errorf("token[%d].CreatedAt = %v, want %v", index, got.CreatedAt, want.CreatedAt)
	}
	if got.CreatedBy != want.CreatedBy {
		t.Errorf("token[%d].CreatedBy = %q, want %q", index, got.CreatedBy, want.CreatedBy)
	}
	assertOpAMPTokenTimeEqual(t, index, "ExpiresAt", got.ExpiresAt, want.ExpiresAt)
	assertOpAMPTokenTimeEqual(t, index, "LastUsedAt", got.LastUsedAt, want.LastUsedAt)
	assertOpAMPTokenTimeEqual(t, index, "RevokedAt", got.RevokedAt, want.RevokedAt)
	if got.RevokedBy != want.RevokedBy {
		t.Errorf("token[%d].RevokedBy = %q, want %q", index, got.RevokedBy, want.RevokedBy)
	}
	if got.Status != want.Status {
		t.Errorf("token[%d].Status = %q, want %q", index, got.Status, want.Status)
	}
}

func assertOpAMPTokenTimeEqual(t *testing.T, index int, field string, got, want *time.Time) {
	t.Helper()
	switch {
	case got == nil && want == nil:
		return
	case got == nil:
		t.Errorf("token[%d].%s = nil, want %v", index, field, *want)
	case want == nil:
		t.Errorf("token[%d].%s = %v, want nil", index, field, *got)
	case !got.Equal(*want):
		t.Errorf("token[%d].%s = %v, want %v", index, field, *got, *want)
	}
}

func TestOpAMPTokenValidationIsReadOnlyAndUsesOneInvalidSentinel(t *testing.T) {
	db := newTestDB(t)
	installOpAMPTokenUpdateCounter(t, db)
	now := time.Date(2026, time.July, 22, 10, 0, 0, 0, time.UTC)
	createdAt := now.Add(-time.Hour)
	expiresLater := now.Add(time.Hour)
	expiresNow := now
	revokedAt := now.Add(-time.Minute)
	revokedBy := "security@example.com"
	activeHash := sha256.Sum256([]byte("active-secret"))
	expiredHash := sha256.Sum256([]byte("expired-secret"))
	revokedHash := sha256.Sum256([]byte("revoked-secret"))
	wrongHash := sha256.Sum256([]byte("wrong-secret"))

	activeID := "00000000-0000-4000-8000-000000000011"
	expiredID := "00000000-0000-4000-8000-000000000012"
	revokedID := "00000000-0000-4000-8000-000000000013"
	seedOpAMPToken(t, db, opAMPTokenFixture{id: activeID, name: "active", secretHash: activeHash, createdAt: createdAt, createdBy: "admin@example.com", expiresAt: &expiresLater})
	seedOpAMPToken(t, db, opAMPTokenFixture{id: expiredID, name: "expired", secretHash: expiredHash, createdAt: createdAt, createdBy: "admin@example.com", expiresAt: &expiresNow})
	seedOpAMPToken(t, db, opAMPTokenFixture{id: revokedID, name: "revoked", secretHash: revokedHash, createdAt: createdAt, createdBy: "admin@example.com", revokedAt: &revokedAt, revokedBy: &revokedBy})

	principal, err := db.ValidateOpAMPToken(context.Background(), activeID, activeHash, now)
	if err != nil {
		t.Fatalf("ValidateOpAMPToken(active) error = %v", err)
	}
	if principal.ID != activeID || principal.ExpiresAt == nil || !principal.ExpiresAt.Equal(expiresLater) {
		t.Fatalf("ValidateOpAMPToken(active) principal = %+v", principal)
	}

	tests := []struct {
		name string
		id   string
		hash [32]byte
	}{
		{name: "absent", id: "00000000-0000-4000-8000-000000000099", hash: wrongHash},
		{name: "wrong_hash", id: activeID, hash: wrongHash},
		{name: "expired", id: expiredID, hash: expiredHash},
		{name: "revoked", id: revokedID, hash: revokedHash},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			principal, err := db.ValidateOpAMPToken(context.Background(), tt.id, tt.hash, now)
			if !errors.Is(err, ext.ErrInvalidOpAMPToken) {
				t.Fatalf("ValidateOpAMPToken() error = %v, want ErrInvalidOpAMPToken", err)
			}
			if principal != (models.OpAMPTokenPrincipal{}) {
				t.Fatalf("ValidateOpAMPToken() principal = %+v, want zero value", principal)
			}
		})
	}

	if got := opAMPTokenUpdateCount(t, db); got != 0 {
		t.Fatalf("validation performed %d token updates, want 0", got)
	}
}

func TestOpAMPTokenMethodsRejectNonCanonicalIDsBeforeSQL(t *testing.T) {
	database, err := sql.Open("pgx", testdb.New(t).DSN)
	if err != nil {
		t.Fatalf("open PostgreSQL handle: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close PostgreSQL handle: %v", err)
	}
	db := &DB{DB: database}
	nonCanonicalID := "00000000-0000-4000-8000-0000000000AA"
	hash := sha256.Sum256([]byte("secret"))

	if _, err := db.ValidateOpAMPToken(context.Background(), nonCanonicalID, hash, time.Now()); !errors.Is(err, ext.ErrInvalidOpAMPToken) {
		t.Fatalf("ValidateOpAMPToken(non-canonical ID) error = %v, want ErrInvalidOpAMPToken", err)
	}
	if err := db.MarkOpAMPTokenUsed(context.Background(), nonCanonicalID, time.Now()); !errors.Is(err, ext.ErrInvalidOpAMPToken) {
		t.Fatalf("MarkOpAMPTokenUsed(non-canonical ID) error = %v, want ErrInvalidOpAMPToken", err)
	}
}

func TestOpAMPTokenMarkUsedWritesOnlyAtInclusiveThirtySecondBoundary(t *testing.T) {
	db := newTestDB(t)
	installOpAMPTokenUpdateCounter(t, db)
	t0 := time.Date(2026, time.July, 22, 10, 0, 0, 0, time.UTC)
	id := "00000000-0000-4000-8000-000000000021"
	seedOpAMPToken(t, db, opAMPTokenFixture{
		id: id, name: "activity", secretHash: sha256.Sum256([]byte("secret")),
		createdAt: t0.Add(-time.Hour), createdBy: "admin@example.com",
	})

	for _, admissionTime := range []time.Time{t0, t0.Add(29 * time.Second), t0.Add(30 * time.Second), t0.Add(10 * time.Second)} {
		if err := db.MarkOpAMPTokenUsed(context.Background(), id, admissionTime); err != nil {
			t.Fatalf("MarkOpAMPTokenUsed(%s) error = %v", admissionTime, err)
		}
	}

	if got := opAMPTokenUpdateCount(t, db); got != 2 {
		t.Fatalf("physical token updates = %d, want 2", got)
	}
	var lastUsedAt time.Time
	if err := db.QueryRow(`SELECT last_used_at FROM opamp_tokens WHERE id = ?`, id).Scan(&lastUsedAt); err != nil {
		t.Fatalf("query last_used_at: %v", err)
	}
	if !lastUsedAt.Equal(t0.Add(30 * time.Second)) {
		t.Fatalf("last_used_at = %v, want %v", lastUsedAt, t0.Add(30*time.Second))
	}
}

func TestOpAMPTokenMarkUsedRejectsInactiveTokensWithoutTouch(t *testing.T) {
	db := newTestDB(t)
	installOpAMPTokenUpdateCounter(t, db)
	now := time.Date(2026, time.July, 22, 10, 0, 0, 0, time.UTC)
	createdAt := now.Add(-time.Hour)
	expiresNow := now
	revokedAt := now.Add(-time.Minute)
	revokedBy := "security@example.com"
	expiredID := "00000000-0000-4000-8000-000000000031"
	revokedID := "00000000-0000-4000-8000-000000000032"
	seedOpAMPToken(t, db, opAMPTokenFixture{id: expiredID, name: "expired", secretHash: sha256.Sum256([]byte("expired")), createdAt: createdAt, createdBy: "admin@example.com", expiresAt: &expiresNow})
	seedOpAMPToken(t, db, opAMPTokenFixture{id: revokedID, name: "revoked", secretHash: sha256.Sum256([]byte("revoked")), createdAt: createdAt, createdBy: "admin@example.com", revokedAt: &revokedAt, revokedBy: &revokedBy})

	for _, id := range []string{expiredID, revokedID, "00000000-0000-4000-8000-000000000099"} {
		if err := db.MarkOpAMPTokenUsed(context.Background(), id, now); !errors.Is(err, ext.ErrInvalidOpAMPToken) {
			t.Fatalf("MarkOpAMPTokenUsed(%s) error = %v, want ErrInvalidOpAMPToken", id, err)
		}
	}
	if got := opAMPTokenUpdateCount(t, db); got != 0 {
		t.Fatalf("inactive admissions performed %d token updates, want 0", got)
	}
}

func TestOpAMPTokenConcurrentAdmissionsWriteOncePerWindow(t *testing.T) {
	db := newTestDBWithPoolConfig(t, PoolConfig{
		MaxOpenConns:    20,
		MaxIdleConns:    10,
		ConnMaxLifetime: time.Minute,
	})
	installOpAMPTokenUpdateCounter(t, db)
	now := time.Date(2026, time.July, 22, 10, 0, 0, 0, time.UTC)
	id := "00000000-0000-4000-8000-000000000041"
	seedOpAMPToken(t, db, opAMPTokenFixture{
		id: id, name: "concurrent", secretHash: sha256.Sum256([]byte("secret")),
		createdAt: now.Add(-time.Hour), createdBy: "admin@example.com",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := make(chan struct{})
	errorsCh := make(chan error, 100)
	var workers sync.WaitGroup
	workers.Add(100)
	startedAt := time.Now()
	for range 100 {
		go func() {
			defer workers.Done()
			<-start
			errorsCh <- db.MarkOpAMPTokenUsed(ctx, id, now)
		}()
	}
	close(start)
	workers.Wait()
	close(errorsCh)

	if elapsed := time.Since(startedAt); elapsed >= 5*time.Second {
		t.Fatalf("100 concurrent admissions took %v, want less than 5s", elapsed)
	}
	for err := range errorsCh {
		if err != nil {
			t.Fatalf("concurrent MarkOpAMPTokenUsed() error = %v", err)
		}
	}
	if got := opAMPTokenUpdateCount(t, db); got != 1 {
		t.Fatalf("physical token updates = %d, want 1", got)
	}
}

type opAMPTokenFixture struct {
	id          string
	name        string
	description string
	team        string
	environment string
	secretHash  [32]byte
	createdAt   time.Time
	createdBy   string
	expiresAt   *time.Time
	lastUsedAt  *time.Time
	revokedAt   *time.Time
	revokedBy   *string
}

func seedOpAMPToken(t *testing.T, db *DB, fixture opAMPTokenFixture) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO opamp_tokens (
			id, name, description, team, environment, secret_hash,
			created_at, created_by, expires_at, last_used_at, revoked_at, revoked_by
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		fixture.id, fixture.name, fixture.description, fixture.team, fixture.environment, fixture.secretHash[:],
		fixture.createdAt, fixture.createdBy, fixture.expiresAt, fixture.lastUsedAt, fixture.revokedAt, fixture.revokedBy,
	); err != nil {
		t.Fatalf("seed OpAMP token %s: %v", fixture.id, err)
	}
}

func installOpAMPTokenUpdateCounter(t *testing.T, db *DB) {
	t.Helper()
	if _, err := db.ExecPostgres(`
		CREATE TABLE opamp_token_update_log (token_id UUID NOT NULL);
		CREATE FUNCTION record_opamp_token_update() RETURNS trigger
		LANGUAGE plpgsql AS $$
		BEGIN
			INSERT INTO opamp_token_update_log (token_id) VALUES (NEW.id);
			RETURN NEW;
		END;
		$$;
		CREATE TRIGGER count_opamp_token_updates
		AFTER UPDATE ON opamp_tokens
		FOR EACH ROW EXECUTE FUNCTION record_opamp_token_update();
	`); err != nil {
		t.Fatalf("install OpAMP token update counter: %v", err)
	}
}

func opAMPTokenUpdateCount(t *testing.T, db *DB) int {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM opamp_token_update_log`).Scan(&count); err != nil {
		t.Fatalf("count OpAMP token updates: %v", err)
	}
	return count
}
