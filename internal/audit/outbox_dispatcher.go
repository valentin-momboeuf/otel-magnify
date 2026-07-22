package audit

import (
	"context"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/magnify-labs/otel-magnify/pkg/ext"
)

const (
	auditOutboxLeaseDuration = 30 * time.Second
	auditOutboxSinkTimeout   = 10 * time.Second
	auditOutboxIdleInterval  = time.Second
	auditOutboxMaxBackoff    = time.Minute
)

// OutboxDispatcher delivers durable audit events to an explicitly configured sink.
type OutboxDispatcher struct {
	store ext.AuditOutboxStore
	sink  ext.AuditLogger
	now   func() time.Time
	newID func() string
}

// NewOutboxDispatcher creates a dispatcher. Call Run from the server lifecycle.
func NewOutboxDispatcher(store ext.AuditOutboxStore, sink ext.AuditLogger) *OutboxDispatcher {
	return &OutboxDispatcher{
		store: store,
		sink:  sink,
		now:   func() time.Time { return time.Now().UTC() },
		newID: uuid.NewString,
	}
}

// Run attempts delivery immediately and waits between idle/error polls.
func (d *OutboxDispatcher) Run(ctx context.Context) {
	if d == nil || d.store == nil || d.sink == nil {
		return
	}
	for {
		if ctx.Err() != nil {
			return
		}
		if d.dispatchOnce(ctx) {
			continue
		}
		timer := time.NewTimer(auditOutboxIdleInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-timer.C:
		}
	}
}

func (d *OutboxDispatcher) dispatchOnce(ctx context.Context) bool {
	claimStartedAt := d.now().UTC()
	claimToken := d.newID()
	pending, err := d.store.ClaimAuditOutboxEvent(
		ctx,
		claimToken,
		claimStartedAt,
		claimStartedAt.Add(auditOutboxLeaseDuration),
	)
	if err != nil {
		log.Printf("audit outbox category=claim_error")
		return false
	}
	if pending == nil {
		return false
	}

	deliveryStartedAt := d.now().UTC()
	nominalSinkDeadline := deliveryStartedAt.Add(auditOutboxSinkTimeout)
	if !pending.LeaseUntil.After(nominalSinkDeadline) {
		if !pending.LeaseUntil.After(deliveryStartedAt) {
			log.Printf("audit outbox event_id=%s action=%s category=expired_claim", pending.Event.EventID, pending.Event.Action)
			return false
		}
		nextAttemptAt := deliveryStartedAt.Add(auditOutboxBackoff(pending.AttemptCount))
		updated, rescheduleErr := d.store.RescheduleAuditOutboxEvent(
			ctx,
			pending.Event.EventID,
			pending.ClaimToken,
			deliveryStartedAt,
			nextAttemptAt,
		)
		switch {
		case rescheduleErr != nil:
			log.Printf("audit outbox event_id=%s action=%s category=reschedule_error", pending.Event.EventID, pending.Event.Action)
			return false
		case !updated:
			log.Printf("audit outbox event_id=%s action=%s category=stale_reschedule", pending.Event.EventID, pending.Event.Action)
			return false
		default:
			log.Printf("audit outbox event_id=%s action=%s category=insufficient_lease", pending.Event.EventID, pending.Event.Action)
			return true
		}
	}

	sinkDeadline := nominalSinkDeadline
	if pending.LeaseUntil.Before(sinkDeadline) {
		sinkDeadline = pending.LeaseUntil
	}
	sinkCtx, cancel := context.WithDeadline(ctx, sinkDeadline)
	err = d.sink.Log(sinkCtx, pending.Event)
	cancel()
	completedAt := d.now().UTC()
	if err != nil {
		nextAttemptAt := completedAt.Add(auditOutboxBackoff(pending.AttemptCount))
		updated, rescheduleErr := d.store.RescheduleAuditOutboxEvent(
			ctx,
			pending.Event.EventID,
			pending.ClaimToken,
			completedAt,
			nextAttemptAt,
		)
		switch {
		case rescheduleErr != nil:
			log.Printf("audit outbox event_id=%s action=%s category=reschedule_error", pending.Event.EventID, pending.Event.Action)
			return false
		case !updated:
			log.Printf("audit outbox event_id=%s action=%s category=stale_reschedule", pending.Event.EventID, pending.Event.Action)
			return false
		default:
			log.Printf("audit outbox event_id=%s action=%s category=sink_error", pending.Event.EventID, pending.Event.Action)
			return true
		}
	}

	updated, err := d.store.MarkAuditOutboxEventDelivered(
		ctx,
		pending.Event.EventID,
		pending.ClaimToken,
		completedAt,
	)
	switch {
	case err != nil:
		log.Printf("audit outbox event_id=%s action=%s category=ack_error", pending.Event.EventID, pending.Event.Action)
		return false
	case !updated:
		log.Printf("audit outbox event_id=%s action=%s category=stale_ack", pending.Event.EventID, pending.Event.Action)
		return false
	default:
		return true
	}
}

func auditOutboxBackoff(attemptCount int) time.Duration {
	if attemptCount <= 1 {
		return time.Second
	}
	delay := time.Second
	for attempt := 1; attempt < attemptCount; attempt++ {
		if delay >= auditOutboxMaxBackoff/2 {
			return auditOutboxMaxBackoff
		}
		delay *= 2
	}
	return delay
}
