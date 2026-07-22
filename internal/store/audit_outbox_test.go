package store

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/magnify-labs/otel-magnify/pkg/ext"
)

func TestAuditOutboxClaimUsesOpaqueTokenAndSkipsConcurrentClaims(t *testing.T) {
	db := newTestDBWithPoolConfig(t, PoolConfig{MaxOpenConns: 8, MaxIdleConns: 4, ConnMaxLifetime: time.Minute})
	now := time.Date(2026, time.July, 22, 12, 0, 0, 0, time.UTC)
	first := atomicAuditEvent("10000000-0000-4000-8000-000000000401", now.Add(-time.Minute), "opamp.token.create", "00000000-0000-4000-8000-000000000401", "user-1")
	second := atomicAuditEvent("10000000-0000-4000-8000-000000000402", now.Add(-time.Minute), "opamp.token.create", "00000000-0000-4000-8000-000000000402", "user-2")
	seedAuditOutboxEvent(t, db, first)
	seedAuditOutboxEvent(t, db, second)

	start := make(chan struct{})
	claimed := make(chan *ext.PendingAuditEvent, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			claimToken := uuid.NewString()
			<-start
			event, err := db.ClaimAuditOutboxEvent(context.Background(), claimToken, now, now.Add(30*time.Second))
			claimed <- event
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(claimed)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("ClaimAuditOutboxEvent() error = %v", err)
		}
	}

	seenEvents := map[string]bool{}
	seenClaims := map[string]bool{}
	for pending := range claimed {
		if pending == nil {
			t.Fatal("ClaimAuditOutboxEvent() returned nil with due events")
		}
		if seenEvents[pending.Event.EventID] {
			t.Fatalf("event %s was claimed twice", pending.Event.EventID)
		}
		seenEvents[pending.Event.EventID] = true
		if _, err := uuid.Parse(pending.ClaimToken); err != nil || pending.ClaimToken != uuid.MustParse(pending.ClaimToken).String() {
			t.Fatalf("claim token %q is not a canonical UUID", pending.ClaimToken)
		}
		if seenClaims[pending.ClaimToken] {
			t.Fatalf("claim token %s was reused", pending.ClaimToken)
		}
		seenClaims[pending.ClaimToken] = true
		if pending.AttemptCount != 1 || !pending.LeaseUntil.Equal(now.Add(30*time.Second)) {
			t.Fatalf("pending event = %+v, want attempt 1 and 30s lease", pending)
		}
	}
	if len(seenEvents) != 2 || len(seenClaims) != 2 {
		t.Fatalf("claimed events=%v claims=%v, want two unique values", seenEvents, seenClaims)
	}
}

func TestAuditOutboxExpiredLeaseCanBeReclaimedAndStaleWorkerCannotFinish(t *testing.T) {
	db := newTestDB(t)
	now := time.Date(2026, time.July, 22, 12, 0, 0, 0, time.UTC)
	event := atomicAuditEvent("10000000-0000-4000-8000-000000000411", now, "opamp.token.create", "00000000-0000-4000-8000-000000000411", "user-1")
	seedAuditOutboxEvent(t, db, event)
	firstClaim := "20000000-0000-4000-8000-000000000411"
	secondClaim := "30000000-0000-4000-8000-000000000411"

	first, err := db.ClaimAuditOutboxEvent(context.Background(), firstClaim, now, now.Add(30*time.Second))
	if err != nil || first == nil {
		t.Fatalf("first claim = (%+v, %v)", first, err)
	}
	reclaimAt := now.Add(30 * time.Second)
	second, err := db.ClaimAuditOutboxEvent(context.Background(), secondClaim, reclaimAt, reclaimAt.Add(30*time.Second))
	if err != nil || second == nil {
		t.Fatalf("second claim = (%+v, %v)", second, err)
	}
	if second.AttemptCount != 2 || second.ClaimToken != secondClaim {
		t.Fatalf("second claim = %+v, want attempt 2 and replacement token", second)
	}

	if ok, err := db.MarkAuditOutboxEventDelivered(context.Background(), event.EventID, firstClaim, reclaimAt.Add(time.Second)); err != nil || ok {
		t.Fatalf("stale MarkDelivered() = (%t, %v), want false, nil", ok, err)
	}
	if ok, err := db.RescheduleAuditOutboxEvent(context.Background(), event.EventID, firstClaim, reclaimAt.Add(time.Second), reclaimAt.Add(time.Minute)); err != nil || ok {
		t.Fatalf("stale Reschedule() = (%t, %v), want false, nil", ok, err)
	}
	if ok, err := db.MarkAuditOutboxEventDelivered(context.Background(), event.EventID, secondClaim, second.LeaseUntil); err != nil || ok {
		t.Fatalf("boundary MarkDelivered() = (%t, %v), want false, nil", ok, err)
	}
	completedAt := second.LeaseUntil.Add(-time.Microsecond)
	if ok, err := db.MarkAuditOutboxEventDelivered(context.Background(), event.EventID, secondClaim, completedAt); err != nil || !ok {
		t.Fatalf("current MarkDelivered() = (%t, %v), want true, nil", ok, err)
	}

	var deliveredAt time.Time
	var count int
	if err := db.QueryRow(`SELECT delivered_at, COUNT(*) OVER () FROM audit_outbox WHERE event_id = ?`, event.EventID).Scan(&deliveredAt, &count); err != nil {
		t.Fatalf("read delivered row: %v", err)
	}
	if count != 1 || !deliveredAt.Equal(completedAt) {
		t.Fatalf("delivered row count/time = %d/%v, want retained row at %v", count, deliveredAt, completedAt)
	}
}

func TestAuditOutboxRescheduleReleasesLeaseAndHonorsNextAttempt(t *testing.T) {
	db := newTestDB(t)
	now := time.Date(2026, time.July, 22, 12, 0, 0, 0, time.UTC)
	event := atomicAuditEvent("10000000-0000-4000-8000-000000000421", now, "opamp.token.create", "00000000-0000-4000-8000-000000000421", "user-1")
	seedAuditOutboxEvent(t, db, event)
	firstClaim := "20000000-0000-4000-8000-000000000421"
	pending, err := db.ClaimAuditOutboxEvent(context.Background(), firstClaim, now, now.Add(30*time.Second))
	if err != nil || pending == nil {
		t.Fatalf("claim = (%+v, %v)", pending, err)
	}
	nextAttemptAt := now.Add(time.Second)
	if ok, err := db.RescheduleAuditOutboxEvent(context.Background(), event.EventID, firstClaim, now.Add(100*time.Millisecond), nextAttemptAt); err != nil || !ok {
		t.Fatalf("Reschedule() = (%t, %v), want true, nil", ok, err)
	}
	if got, err := db.ClaimAuditOutboxEvent(context.Background(), uuid.NewString(), nextAttemptAt.Add(-time.Nanosecond), nextAttemptAt.Add(30*time.Second)); err != nil || got != nil {
		t.Fatalf("early Claim() = (%+v, %v), want nil, nil", got, err)
	}
	secondClaim := uuid.NewString()
	got, err := db.ClaimAuditOutboxEvent(context.Background(), secondClaim, nextAttemptAt, nextAttemptAt.Add(30*time.Second))
	if err != nil || got == nil {
		t.Fatalf("due Claim() = (%+v, %v)", got, err)
	}
	if got.AttemptCount != 2 || got.ClaimToken != secondClaim {
		t.Fatalf("due claim = %+v, want attempt 2", got)
	}

	var hasErrorColumn bool
	if err := db.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = current_schema()
			  AND table_name = 'audit_outbox'
			  AND column_name LIKE '%error%'
		)`).Scan(&hasErrorColumn); err != nil {
		t.Fatalf("inspect outbox columns: %v", err)
	}
	if hasErrorColumn {
		t.Fatal("audit_outbox persists sink error text")
	}
}
