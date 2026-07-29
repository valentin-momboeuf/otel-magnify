package audit

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/magnify-labs/otel-magnify/internal/store"
	"github.com/magnify-labs/otel-magnify/internal/testdb"
	"github.com/magnify-labs/otel-magnify/pkg/ext"
)

type dispatcherStore struct {
	mu sync.Mutex

	events          []*ext.PendingAuditEvent
	claimCalls      int
	claimTokens     []string
	claimNow        []time.Time
	claimLease      []time.Time
	delivered       []dispatcherCompletion
	rescheduled     []dispatcherReschedule
	claimErr        error
	deliveredOkay   bool
	deliveredErr    error
	rescheduleErr   error
	rescheduleStale bool
	onClaim         func()
}

type dispatcherCompletion struct {
	eventID, claimToken string
	completedAt         time.Time
}

type dispatcherReschedule struct {
	dispatcherCompletion
	nextAttemptAt time.Time
}

func (s *dispatcherStore) ClaimAuditOutboxEvent(_ context.Context, claimToken string, now, leaseUntil time.Time) (*ext.PendingAuditEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.claimCalls++
	s.claimTokens = append(s.claimTokens, claimToken)
	s.claimNow = append(s.claimNow, now)
	s.claimLease = append(s.claimLease, leaseUntil)
	if s.claimErr != nil {
		return nil, s.claimErr
	}
	if len(s.events) == 0 {
		return nil, nil
	}
	event := *s.events[0]
	s.events = s.events[1:]
	event.ClaimToken = claimToken
	event.LeaseUntil = leaseUntil
	if s.onClaim != nil {
		s.onClaim()
	}
	return &event, nil
}

func (s *dispatcherStore) MarkAuditOutboxEventDelivered(_ context.Context, eventID, claimToken string, completedAt time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.delivered = append(s.delivered, dispatcherCompletion{eventID: eventID, claimToken: claimToken, completedAt: completedAt})
	return s.deliveredOkay, s.deliveredErr
}

func (s *dispatcherStore) RescheduleAuditOutboxEvent(_ context.Context, eventID, claimToken string, completedAt, nextAttemptAt time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rescheduled = append(s.rescheduled, dispatcherReschedule{
		dispatcherCompletion: dispatcherCompletion{eventID: eventID, claimToken: claimToken, completedAt: completedAt},
		nextAttemptAt:        nextAttemptAt,
	})
	if s.rescheduleErr != nil {
		return false, s.rescheduleErr
	}
	return !s.rescheduleStale, nil
}

type dispatcherSink struct {
	mu        sync.Mutex
	events    []ext.AuditEvent
	deadlines []time.Time
	err       error
}

func (s *dispatcherSink) Log(ctx context.Context, event ext.AuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
	deadline, _ := ctx.Deadline()
	s.deadlines = append(s.deadlines, deadline)
	return s.err
}

func TestAuditOutboxDispatcherClaimsWithFreshUUIDThirtySecondLeaseAndTenSecondSinkTimeout(t *testing.T) {
	startedAt := time.Now().UTC()
	deliveryStartedAt := startedAt.Add(time.Second)
	currentTime := startedAt
	event := ext.AuditEvent{
		EventID: "10000000-0000-4000-8000-000000000501", OccurredAt: startedAt.Add(-time.Minute),
		Action: "opamp.token.create", UserID: "user-1", Email: "user-1@example.com",
		Resource: "opamp_token", ResourceID: "00000000-0000-4000-8000-000000000501",
	}
	store := &dispatcherStore{
		events:        []*ext.PendingAuditEvent{{Event: event, AttemptCount: 1}},
		deliveredOkay: true,
		onClaim:       func() { currentTime = deliveryStartedAt },
	}
	sink := &dispatcherSink{}
	dispatcher := NewOutboxDispatcher(store, sink)
	dispatcher.now = func() time.Time { return currentTime }

	if worked := dispatcher.dispatchOnce(context.Background()); !worked {
		t.Fatal("dispatchOnce() = false, want true")
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.claimTokens) != 1 {
		t.Fatalf("claim token count = %d, want 1", len(store.claimTokens))
	}
	claimToken := store.claimTokens[0]
	parsed, err := uuid.Parse(claimToken)
	if err != nil || parsed.String() != claimToken {
		t.Fatalf("claim token = %q, want canonical UUID", claimToken)
	}
	if got := store.claimLease[0].Sub(store.claimNow[0]); got != 30*time.Second {
		t.Fatalf("lease duration = %v, want 30s", got)
	}
	if len(store.delivered) != 1 || store.delivered[0].claimToken != claimToken || store.delivered[0].eventID != event.EventID {
		t.Fatalf("delivered calls = %+v, want current event and claim", store.delivered)
	}

	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.events) != 1 || sink.events[0] != event {
		t.Fatalf("sink events = %+v, want exact event %+v", sink.events, event)
	}
	if want := deliveryStartedAt.Add(10 * time.Second); !sink.deadlines[0].Equal(want) {
		t.Fatalf("sink deadline = %v, want %v", sink.deadlines[0], want)
	}
}

func TestAuditOutboxDispatcherSkipsSinkWhenClaimConsumesLeaseBudget(t *testing.T) {
	tests := []struct {
		name            string
		delay           time.Duration
		wantRescheduled int
		wantContinueNow bool
	}{
		{name: "claim_still_valid_but_too_short_for_sink", delay: 20 * time.Second, wantRescheduled: 1, wantContinueNow: true},
		{name: "claim_already_expired", delay: 30 * time.Second, wantRescheduled: 0, wantContinueNow: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claimStartedAt := time.Date(2026, time.July, 22, 12, 0, 0, 0, time.UTC)
			currentTime := claimStartedAt
			event := ext.AuditEvent{
				EventID: "10000000-0000-4000-8000-000000000541", OccurredAt: claimStartedAt.Add(-time.Minute),
				Action: "opamp.token.create", Resource: "opamp_token",
			}
			store := &dispatcherStore{
				events:  []*ext.PendingAuditEvent{{Event: event, AttemptCount: 1}},
				onClaim: func() { currentTime = claimStartedAt.Add(tt.delay) },
			}
			sink := &dispatcherSink{}
			dispatcher := NewOutboxDispatcher(store, sink)
			dispatcher.now = func() time.Time { return currentTime }

			if got := dispatcher.dispatchOnce(context.Background()); got != tt.wantContinueNow {
				t.Fatalf("dispatchOnce() = %t, want %t", got, tt.wantContinueNow)
			}
			sink.mu.Lock()
			sinkCalls := len(sink.events)
			sink.mu.Unlock()
			if sinkCalls != 0 {
				t.Fatalf("sink call count = %d, want 0 with insufficient lease budget", sinkCalls)
			}
			store.mu.Lock()
			defer store.mu.Unlock()
			if len(store.delivered) != 0 {
				t.Fatalf("delivery calls = %+v, want none", store.delivered)
			}
			if len(store.rescheduled) != tt.wantRescheduled {
				t.Fatalf("reschedule calls = %d, want %d", len(store.rescheduled), tt.wantRescheduled)
			}
			if tt.wantRescheduled == 1 && !store.rescheduled[0].completedAt.Equal(currentTime) {
				t.Fatalf("reschedule completed_at = %v, want %v", store.rescheduled[0].completedAt, currentTime)
			}
		})
	}
}

func TestAuditOutboxDispatcherReschedulesWithExponentialCappedBackoff(t *testing.T) {
	for _, tt := range []struct {
		attempt int
		want    time.Duration
	}{
		{attempt: 1, want: time.Second},
		{attempt: 2, want: 2 * time.Second},
		{attempt: 6, want: 32 * time.Second},
		{attempt: 7, want: time.Minute},
		{attempt: 100, want: time.Minute},
	} {
		if got := auditOutboxBackoff(tt.attempt); got != tt.want {
			t.Errorf("auditOutboxBackoff(%d) = %v, want %v", tt.attempt, got, tt.want)
		}
	}

	now := time.Date(2026, time.July, 22, 12, 0, 0, 0, time.UTC)
	event := ext.AuditEvent{EventID: "10000000-0000-4000-8000-000000000511", OccurredAt: now.Add(-time.Minute), Action: "opamp.token.create", Resource: "opamp_token"}
	store := &dispatcherStore{events: []*ext.PendingAuditEvent{{Event: event, AttemptCount: 3}}}
	sink := &dispatcherSink{err: errors.New("secret sink failure: credential=do-not-log")}
	dispatcher := NewOutboxDispatcher(store, sink)
	dispatcher.now = func() time.Time { return now }
	if worked := dispatcher.dispatchOnce(context.Background()); !worked {
		t.Fatal("dispatchOnce() = false, want true")
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.rescheduled) != 1 {
		t.Fatalf("reschedule calls = %d, want 1", len(store.rescheduled))
	}
	if got := store.rescheduled[0].nextAttemptAt.Sub(now); got != 4*time.Second {
		t.Fatalf("reschedule backoff = %v, want 4s", got)
	}
}

func TestAuditOutboxDispatcherSinkErrorLogRedactsErrorText(t *testing.T) {
	now := time.Now().UTC()
	event := ext.AuditEvent{
		EventID: "10000000-0000-4000-8000-000000000551", OccurredAt: now.Add(-time.Minute),
		Action: "opamp.token.create", Resource: "opamp_token",
	}
	store := &dispatcherStore{events: []*ext.PendingAuditEvent{{Event: event, AttemptCount: 1}}}
	sink := &dispatcherSink{err: errors.New("sink rejected bearer raw-token-secret")}
	dispatcher := NewOutboxDispatcher(store, sink)
	dispatcher.now = func() time.Time { return now }

	var logs bytes.Buffer
	previousOutput := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(previousOutput)
	dispatcher.dispatchOnce(context.Background())

	got := logs.String()
	if strings.Contains(got, "raw-token-secret") || strings.Contains(got, "sink rejected bearer") {
		t.Fatalf("dispatcher log exposes sink error text: %q", got)
	}
	if !strings.Contains(got, "category=sink_error") {
		t.Fatalf("dispatcher log = %q, want sink_error category", got)
	}
}

func TestAuditOutboxDispatcherDispatchOnceWaitsAfterStaleOrFailedCompletion(t *testing.T) {
	now := time.Now().UTC()
	event := ext.AuditEvent{
		EventID: "10000000-0000-4000-8000-000000000552", OccurredAt: now.Add(-time.Minute),
		Action: "opamp.token.create", Resource: "opamp_token",
	}
	tests := []struct {
		name  string
		store *dispatcherStore
		sink  *dispatcherSink
	}{
		{
			name:  "ack_stale",
			store: &dispatcherStore{events: []*ext.PendingAuditEvent{{Event: event, AttemptCount: 1}}},
			sink:  &dispatcherSink{},
		},
		{
			name: "ack_error",
			store: &dispatcherStore{
				events:       []*ext.PendingAuditEvent{{Event: event, AttemptCount: 1}},
				deliveredErr: errors.New("ack unavailable"),
			},
			sink: &dispatcherSink{},
		},
		{
			name: "reschedule_stale",
			store: &dispatcherStore{
				events:          []*ext.PendingAuditEvent{{Event: event, AttemptCount: 1}},
				rescheduleStale: true,
			},
			sink: &dispatcherSink{err: errors.New("sink unavailable")},
		},
		{
			name: "reschedule_error",
			store: &dispatcherStore{
				events:        []*ext.PendingAuditEvent{{Event: event, AttemptCount: 1}},
				rescheduleErr: errors.New("store unavailable"),
			},
			sink: &dispatcherSink{err: errors.New("sink unavailable")},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dispatcher := NewOutboxDispatcher(tt.store, tt.sink)
			dispatcher.now = func() time.Time { return now }
			if got := dispatcher.dispatchOnce(context.Background()); got {
				t.Fatal("dispatchOnce() = true, want false so Run waits for the idle interval")
			}
		})
	}
}

func TestAuditOutboxDispatcherRetryPreservesEventIdentityAndUsesFreshClaim(t *testing.T) {
	now := time.Date(2026, time.July, 22, 12, 0, 0, 0, time.UTC)
	event := ext.AuditEvent{EventID: "10000000-0000-4000-8000-000000000521", OccurredAt: now.Add(-time.Minute), Action: "opamp.token.revoke", Resource: "opamp_token"}
	store := &dispatcherStore{
		events:        []*ext.PendingAuditEvent{{Event: event, AttemptCount: 1}, {Event: event, AttemptCount: 2}},
		deliveredOkay: false,
	}
	sink := &dispatcherSink{}
	dispatcher := NewOutboxDispatcher(store, sink)
	dispatcher.now = func() time.Time { return now }

	dispatcher.dispatchOnce(context.Background())
	dispatcher.dispatchOnce(context.Background())

	store.mu.Lock()
	if len(store.claimTokens) != 2 || store.claimTokens[0] == store.claimTokens[1] {
		store.mu.Unlock()
		t.Fatalf("claim tokens = %v, want two fresh values", store.claimTokens)
	}
	store.mu.Unlock()
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.events) != 2 || sink.events[0].EventID != event.EventID || sink.events[1].EventID != event.EventID ||
		!sink.events[0].OccurredAt.Equal(event.OccurredAt) || !sink.events[1].OccurredAt.Equal(event.OccurredAt) {
		t.Fatalf("retried events = %+v, want stable ID and timestamp", sink.events)
	}
}

type dropFirstAckStore struct {
	delegate ext.AuditOutboxStore
	dropped  bool
}

func (s *dropFirstAckStore) ClaimAuditOutboxEvent(ctx context.Context, claimToken string, now, leaseUntil time.Time) (*ext.PendingAuditEvent, error) {
	return s.delegate.ClaimAuditOutboxEvent(ctx, claimToken, now, leaseUntil)
}

func (s *dropFirstAckStore) MarkAuditOutboxEventDelivered(ctx context.Context, eventID, claimToken string, completedAt time.Time) (bool, error) {
	if !s.dropped {
		s.dropped = true
		return false, nil
	}
	return s.delegate.MarkAuditOutboxEventDelivered(ctx, eventID, claimToken, completedAt)
}

func (s *dropFirstAckStore) RescheduleAuditOutboxEvent(ctx context.Context, eventID, claimToken string, completedAt, nextAttemptAt time.Time) (bool, error) {
	return s.delegate.RescheduleAuditOutboxEvent(ctx, eventID, claimToken, completedAt, nextAttemptAt)
}

func TestAuditOutboxDispatcherReplaysAfterSinkSuccessAndMissingAck(t *testing.T) {
	db, err := store.Open(testdb.New(t).DSN, store.PoolConfig{
		MaxOpenConns: 4, MaxIdleConns: 2, ConnMaxLifetime: time.Minute,
	})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate store: %v", err)
	}
	now := time.Date(2026, time.July, 22, 12, 0, 0, 0, time.UTC)
	event := ext.AuditEvent{
		EventID: "10000000-0000-4000-8000-000000000531", OccurredAt: now.Add(-time.Minute),
		Action: "opamp.token.create", UserID: "user-1", Email: "user-1@example.com",
		Resource: "opamp_token", ResourceID: "00000000-0000-4000-8000-000000000531",
	}
	if _, err := db.Exec(`
		INSERT INTO audit_outbox (
			event_id, occurred_at, action, user_id, email, resource, resource_id, detail, next_attempt_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.EventID, event.OccurredAt, event.Action, event.UserID, event.Email,
		event.Resource, event.ResourceID, event.Detail, event.OccurredAt,
	); err != nil {
		t.Fatalf("seed outbox: %v", err)
	}

	storeWithLostAck := &dropFirstAckStore{delegate: db}
	sink := &dispatcherSink{}
	dispatcher := NewOutboxDispatcher(storeWithLostAck, sink)
	currentTime := now
	dispatcher.now = func() time.Time { return currentTime }
	dispatcher.dispatchOnce(context.Background())
	currentTime = now.Add(31 * time.Second)
	dispatcher.dispatchOnce(context.Background())

	sink.mu.Lock()
	if len(sink.events) != 2 || sink.events[0] != event || sink.events[1] != event {
		sink.mu.Unlock()
		t.Fatalf("sink events = %+v, want exact replay twice", sink.events)
	}
	sink.mu.Unlock()
	var attempts int
	var deliveredAt time.Time
	if err := db.QueryRow(`SELECT attempt_count, delivered_at FROM audit_outbox WHERE event_id = ?`, event.EventID).Scan(&attempts, &deliveredAt); err != nil {
		t.Fatalf("read replayed event: %v", err)
	}
	if attempts != 2 || !deliveredAt.Equal(currentTime) {
		t.Fatalf("attempts/delivered_at = %d/%v, want 2/%v", attempts, deliveredAt, currentTime)
	}
}

func TestAuditOutboxDispatcherIdlePollingIsContextAware(t *testing.T) {
	store := &dispatcherStore{}
	dispatcher := NewOutboxDispatcher(store, &dispatcherSink{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		dispatcher.Run(ctx)
		close(done)
	}()

	deadline := time.Now().Add(500 * time.Millisecond)
	for {
		store.mu.Lock()
		calls := store.claimCalls
		store.mu.Unlock()
		if calls >= 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("dispatcher did not attempt an immediate claim")
		}
		time.Sleep(time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("dispatcher did not stop after cancellation")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.claimCalls != 1 {
		t.Fatalf("idle claim calls in less than one second = %d, want 1", store.claimCalls)
	}
}

type repeatingAckStaleStore struct {
	mu         sync.Mutex
	event      ext.AuditEvent
	claimCalls int
	claims     chan int
	acks       chan struct{}
}

func (s *repeatingAckStaleStore) ClaimAuditOutboxEvent(_ context.Context, claimToken string, _ time.Time, leaseUntil time.Time) (*ext.PendingAuditEvent, error) {
	s.mu.Lock()
	s.claimCalls++
	claimNumber := s.claimCalls
	s.mu.Unlock()
	s.claims <- claimNumber
	return &ext.PendingAuditEvent{
		Event: s.event, AttemptCount: claimNumber, ClaimToken: claimToken, LeaseUntil: leaseUntil,
	}, nil
}

func (s *repeatingAckStaleStore) MarkAuditOutboxEventDelivered(context.Context, string, string, time.Time) (bool, error) {
	s.acks <- struct{}{}
	return false, nil
}

func (s *repeatingAckStaleStore) RescheduleAuditOutboxEvent(context.Context, string, string, time.Time, time.Time) (bool, error) {
	return false, nil
}

func TestAuditOutboxDispatcherRunWaitsAfterStaleAck(t *testing.T) {
	now := time.Now().UTC()
	store := &repeatingAckStaleStore{
		event: ext.AuditEvent{
			EventID: "10000000-0000-4000-8000-000000000561", OccurredAt: now.Add(-time.Minute),
			Action: "opamp.token.revoke", Resource: "opamp_token",
		},
		claims: make(chan int, 4),
		acks:   make(chan struct{}, 4),
	}
	dispatcher := NewOutboxDispatcher(store, &dispatcherSink{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		dispatcher.Run(ctx)
		close(done)
	}()

	if claim := <-store.claims; claim != 1 {
		cancel()
		t.Fatalf("first claim number = %d, want 1", claim)
	}
	<-store.acks
	select {
	case claim := <-store.claims:
		cancel()
		t.Fatalf("dispatcher immediately reclaimed after stale ack: claim %d", claim)
	case <-time.After(100 * time.Millisecond):
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("dispatcher did not stop after cancellation")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.claimCalls != 1 {
		t.Fatalf("claim calls before idle interval = %d, want 1", store.claimCalls)
	}
}
