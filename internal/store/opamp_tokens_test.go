package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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

func TestOpAMPTokenAtomicAuditCreateCommitsTokenAndExactEventTogether(t *testing.T) {
	db := newTestDB(t)
	createdAt := time.Date(2026, time.July, 22, 11, 0, 0, 0, time.UTC)
	credential := atomicAuditCredential("00000000-0000-4000-8000-000000000201", createdAt, "user-creator")
	event := atomicAuditEvent(
		"10000000-0000-4000-8000-000000000201",
		createdAt,
		"opamp.token.create",
		credential.Token.ID,
		"user-creator",
	)

	if err := db.CreateOpAMPToken(context.Background(), credential, event); err != nil {
		t.Fatalf("CreateOpAMPToken() error = %v", err)
	}

	var tokenCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM opamp_tokens WHERE id = ?`, credential.Token.ID).Scan(&tokenCount); err != nil {
		t.Fatalf("count token: %v", err)
	}
	if tokenCount != 1 {
		t.Fatalf("token count = %d, want 1", tokenCount)
	}
	got := readAuditOutboxEvent(t, db, event.EventID)
	if got != event {
		t.Fatalf("outbox event = %+v, want %+v", got, event)
	}
	var nextAttemptAt time.Time
	if err := db.QueryRow(`SELECT next_attempt_at FROM audit_outbox WHERE event_id = ?`, event.EventID).Scan(&nextAttemptAt); err != nil {
		t.Fatalf("read next_attempt_at: %v", err)
	}
	if !nextAttemptAt.Equal(event.OccurredAt) {
		t.Fatalf("next_attempt_at = %v, want %v", nextAttemptAt, event.OccurredAt)
	}

	var storedHash []byte
	if err := db.QueryRow(`SELECT secret_hash FROM opamp_tokens WHERE id = ?`, credential.Token.ID).Scan(&storedHash); err != nil {
		t.Fatalf("read secret_hash: %v", err)
	}
	if !bytes.Equal(storedHash, credential.SecretHash[:]) {
		t.Fatal("stored token hash does not match credential")
	}
	serialized, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	if strings.Contains(string(serialized), hex.EncodeToString(credential.SecretHash[:])) || got.Detail != "" {
		t.Fatalf("audit payload contains a secret/hash/detail: %s", serialized)
	}
}

func TestOpAMPTokenAtomicAuditCreateRollsBackWhenOutboxInsertFails(t *testing.T) {
	db := newTestDB(t)
	createdAt := time.Date(2026, time.July, 22, 11, 0, 0, 0, time.UTC)
	existingEvent := atomicAuditEvent(
		"10000000-0000-4000-8000-000000000211",
		createdAt,
		"opamp.token.create",
		"00000000-0000-4000-8000-000000000210",
		"user-creator",
	)
	seedAuditOutboxEvent(t, db, existingEvent)

	credential := atomicAuditCredential("00000000-0000-4000-8000-000000000211", createdAt, "user-creator")
	event := atomicAuditEvent(existingEvent.EventID, createdAt, "opamp.token.create", credential.Token.ID, "user-creator")
	if err := db.CreateOpAMPToken(context.Background(), credential, event); err == nil {
		t.Fatal("CreateOpAMPToken() error = nil, want duplicate outbox failure")
	}

	var tokenCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM opamp_tokens WHERE id = ?`, credential.Token.ID).Scan(&tokenCount); err != nil {
		t.Fatalf("count token: %v", err)
	}
	if tokenCount != 0 {
		t.Fatalf("token count = %d, want 0 after rollback", tokenCount)
	}
}

func TestOpAMPTokenAtomicAuditRevokeCommitsOnceAndRollsBackOnOutboxFailure(t *testing.T) {
	t.Run("success_and_repeat", func(t *testing.T) {
		db := newTestDB(t)
		createdAt := time.Date(2026, time.July, 22, 10, 0, 0, 0, time.UTC)
		now := createdAt.Add(time.Hour)
		credential := atomicAuditCredential("00000000-0000-4000-8000-000000000221", createdAt, "user-creator")
		seedOpAMPToken(t, db, opAMPTokenFixture{
			id: credential.Token.ID, name: credential.Token.Name, secretHash: credential.SecretHash,
			createdAt: createdAt, createdBy: credential.Token.CreatedBy,
		})
		event := atomicAuditEvent("10000000-0000-4000-8000-000000000221", now, "opamp.token.revoke", credential.Token.ID, "user-revoker")

		got, changed, err := db.RevokeOpAMPToken(context.Background(), credential.Token.ID, "user-revoker", now, event)
		if err != nil {
			t.Fatalf("RevokeOpAMPToken() error = %v", err)
		}
		if !changed || got.RevokedAt == nil || !got.RevokedAt.Equal(now) || got.RevokedBy != "user-revoker" {
			t.Fatalf("RevokeOpAMPToken() = (%+v, %t), want revoked transition", got, changed)
		}
		if readAuditOutboxEvent(t, db, event.EventID) != event {
			t.Fatal("stored revoke event differs from input")
		}

		repeatedEvent := atomicAuditEvent("10000000-0000-4000-8000-000000000222", now.Add(time.Minute), "opamp.token.revoke", credential.Token.ID, "user-revoker")
		got, changed, err = db.RevokeOpAMPToken(context.Background(), credential.Token.ID, "user-revoker", now.Add(time.Minute), repeatedEvent)
		if err != nil {
			t.Fatalf("repeated RevokeOpAMPToken() error = %v", err)
		}
		if changed || got.RevokedAt == nil || !got.RevokedAt.Equal(now) {
			t.Fatalf("repeated RevokeOpAMPToken() = (%+v, %t), want unchanged original token", got, changed)
		}
		var repeatedCount int
		if err := db.QueryRow(`SELECT COUNT(*) FROM audit_outbox WHERE event_id = ?`, repeatedEvent.EventID).Scan(&repeatedCount); err != nil {
			t.Fatalf("count repeated event: %v", err)
		}
		if repeatedCount != 0 {
			t.Fatalf("repeated event count = %d, want 0", repeatedCount)
		}
	})

	t.Run("outbox_failure_rolls_back", func(t *testing.T) {
		db := newTestDB(t)
		createdAt := time.Date(2026, time.July, 22, 10, 0, 0, 0, time.UTC)
		now := createdAt.Add(time.Hour)
		id := "00000000-0000-4000-8000-000000000223"
		seedOpAMPToken(t, db, opAMPTokenFixture{id: id, name: "rollback", secretHash: sha256.Sum256([]byte("secret")), createdAt: createdAt, createdBy: "user-creator"})
		event := atomicAuditEvent("10000000-0000-4000-8000-000000000223", now, "opamp.token.revoke", id, "user-revoker")
		seedAuditOutboxEvent(t, db, event)

		if _, _, err := db.RevokeOpAMPToken(context.Background(), id, "user-revoker", now, event); err == nil {
			t.Fatal("RevokeOpAMPToken() error = nil, want duplicate outbox failure")
		}
		var revokedAt sql.NullTime
		if err := db.QueryRow(`SELECT revoked_at FROM opamp_tokens WHERE id = ?`, id).Scan(&revokedAt); err != nil {
			t.Fatalf("read revoked_at: %v", err)
		}
		if revokedAt.Valid {
			t.Fatalf("revoked_at = %v, want NULL after rollback", revokedAt.Time)
		}
	})
}

func TestOpAMPTokenAtomicAuditConcurrentRevocationsCreateOneEvent(t *testing.T) {
	db := newTestDBWithPoolConfig(t, PoolConfig{MaxOpenConns: 20, MaxIdleConns: 10, ConnMaxLifetime: time.Minute})
	createdAt := time.Date(2026, time.July, 22, 10, 0, 0, 0, time.UTC)
	now := createdAt.Add(time.Hour)
	id := "00000000-0000-4000-8000-000000000231"
	seedOpAMPToken(t, db, opAMPTokenFixture{id: id, name: "concurrent-revoke", secretHash: sha256.Sum256([]byte("secret")), createdAt: createdAt, createdBy: "user-creator"})

	const workers = 32
	start := make(chan struct{})
	results := make(chan bool, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	wg.Add(workers)
	for index := range workers {
		go func() {
			defer wg.Done()
			<-start
			eventID := "10000000-0000-4000-8000-" + fmt.Sprintf("%012d", 300+index)
			event := atomicAuditEvent(eventID, now, "opamp.token.revoke", id, "user-revoker")
			_, changed, err := db.RevokeOpAMPToken(context.Background(), id, "user-revoker", now, event)
			results <- changed
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)

	changedCount := 0
	for changed := range results {
		if changed {
			changedCount++
		}
	}
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent RevokeOpAMPToken() error = %v", err)
		}
	}
	if changedCount != 1 {
		t.Fatalf("changed count = %d, want 1", changedCount)
	}
	var eventCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM audit_outbox WHERE resource_id = ? AND action = 'opamp.token.revoke'`, id).Scan(&eventCount); err != nil {
		t.Fatalf("count revoke events: %v", err)
	}
	if eventCount != 1 {
		t.Fatalf("revoke event count = %d, want 1", eventCount)
	}
}

func TestOpAMPTokenAtomicAuditRejectsInvalidEventBeforeSQL(t *testing.T) {
	database, err := sql.Open("pgx", testdb.New(t).DSN)
	if err != nil {
		t.Fatalf("open PostgreSQL handle: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close PostgreSQL handle: %v", err)
	}
	db := &DB{DB: database}
	createdAt := time.Date(2026, time.July, 22, 11, 0, 0, 0, time.UTC)
	credential := atomicAuditCredential("00000000-0000-4000-8000-000000000241", createdAt, "user-creator")
	valid := atomicAuditEvent("10000000-0000-4000-8000-000000000241", createdAt, "opamp.token.create", credential.Token.ID, credential.Token.CreatedBy)

	tests := []struct {
		name   string
		mutate func(*ext.AuditEvent)
	}{
		{name: "event_id_absent", mutate: func(event *ext.AuditEvent) { event.EventID = "" }},
		{name: "event_id_non_canonical", mutate: func(event *ext.AuditEvent) { event.EventID = "10000000-0000-4000-8000-000000000ABC" }},
		{name: "occurred_at_absent", mutate: func(event *ext.AuditEvent) { event.OccurredAt = time.Time{} }},
		{name: "occurred_at_not_utc", mutate: func(event *ext.AuditEvent) {
			event.OccurredAt = event.OccurredAt.In(time.FixedZone("offset", 3600))
		}},
		{name: "occurred_at_inconsistent", mutate: func(event *ext.AuditEvent) { event.OccurredAt = event.OccurredAt.Add(time.Second) }},
		{name: "legacy_action_spelling", mutate: func(event *ext.AuditEvent) { event.Action = "opamp_token.create" }},
		{name: "wrong_resource", mutate: func(event *ext.AuditEvent) { event.Resource = "token" }},
		{name: "wrong_resource_id", mutate: func(event *ext.AuditEvent) { event.ResourceID = "00000000-0000-4000-8000-000000000242" }},
		{name: "wrong_actor", mutate: func(event *ext.AuditEvent) { event.UserID = "user-other" }},
		{name: "detail_present", mutate: func(event *ext.AuditEvent) { event.Detail = "must stay redacted" }},
		{name: "newline", mutate: func(event *ext.AuditEvent) { event.Email = "user@example.com\nforged" }},
		{name: "payload_too_long", mutate: func(event *ext.AuditEvent) { event.Email = strings.Repeat("e", 321) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := valid
			tt.mutate(&event)
			err := db.CreateOpAMPToken(context.Background(), credential, event)
			if err == nil {
				t.Fatal("CreateOpAMPToken() error = nil, want validation error")
			}
			if strings.Contains(err.Error(), "database is closed") {
				t.Fatalf("CreateOpAMPToken() reached SQL before validation: %v", err)
			}
		})
	}
}

func atomicAuditCredential(id string, createdAt time.Time, createdBy string) models.OpAMPTokenCredential {
	return models.OpAMPTokenCredential{
		Token: models.OpAMPToken{
			ID: id, Name: "automation", Description: "collector automation", Team: "platform",
			Environment: "production", CreatedAt: createdAt, CreatedBy: createdBy,
		},
		SecretHash: sha256.Sum256([]byte("opaque-secret-" + id)),
	}
}

func atomicAuditEvent(eventID string, occurredAt time.Time, action, resourceID, userID string) ext.AuditEvent {
	return ext.AuditEvent{
		EventID: eventID, OccurredAt: occurredAt, Action: action, UserID: userID,
		Email: userID + "@example.com", Resource: "opamp_token", ResourceID: resourceID,
	}
}

func seedAuditOutboxEvent(t *testing.T, db *DB, event ext.AuditEvent) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO audit_outbox (
			event_id, occurred_at, action, user_id, email, resource, resource_id, detail, next_attempt_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.EventID, event.OccurredAt, event.Action, event.UserID, event.Email,
		event.Resource, event.ResourceID, event.Detail, event.OccurredAt,
	); err != nil {
		t.Fatalf("seed audit outbox event %s: %v", event.EventID, err)
	}
}

func readAuditOutboxEvent(t *testing.T, db *DB, eventID string) ext.AuditEvent {
	t.Helper()
	var event ext.AuditEvent
	if err := db.QueryRow(`
		SELECT event_id, occurred_at, action, user_id, email, resource, resource_id, detail
		FROM audit_outbox
		WHERE event_id = ?`, eventID,
	).Scan(
		&event.EventID, &event.OccurredAt, &event.Action, &event.UserID,
		&event.Email, &event.Resource, &event.ResourceID, &event.Detail,
	); err != nil {
		t.Fatalf("read audit outbox event %s: %v", eventID, err)
	}
	event.OccurredAt = event.OccurredAt.UTC()
	return event
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
