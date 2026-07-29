package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/magnify-labs/otel-magnify/pkg/ext"
)

// ClaimAuditOutboxEvent atomically leases the next due event to one delivery attempt.
func (d *DB) ClaimAuditOutboxEvent(ctx context.Context, claimToken string, now, leaseUntil time.Time) (*ext.PendingAuditEvent, error) {
	if !isCanonicalUUID(claimToken) {
		return nil, fmt.Errorf("claim audit outbox event: claim token must be a canonical UUID")
	}
	if err := validateUTCTime("now", now); err != nil {
		return nil, fmt.Errorf("claim audit outbox event: %w", err)
	}
	if err := validateUTCTime("lease_until", leaseUntil); err != nil {
		return nil, fmt.Errorf("claim audit outbox event: %w", err)
	}
	if !leaseUntil.After(now) {
		return nil, fmt.Errorf("claim audit outbox event: lease_until must be after now")
	}

	var pending ext.PendingAuditEvent
	err := d.DB.QueryRowContext(ctx, `
		WITH candidate AS (
			SELECT event_id
			FROM audit_outbox
			WHERE delivered_at IS NULL
			  AND next_attempt_at <= $2
			  AND (claim_token IS NULL OR lease_until <= $2)
			ORDER BY next_attempt_at, occurred_at, event_id
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		UPDATE audit_outbox AS outbox
		SET claim_token = $1,
		    lease_until = $3,
		    attempt_count = outbox.attempt_count + 1
		FROM candidate
		WHERE outbox.event_id = candidate.event_id
		RETURNING outbox.event_id, outbox.occurred_at, outbox.action, outbox.user_id,
		          outbox.email, outbox.resource, outbox.resource_id, outbox.detail,
		          outbox.attempt_count, outbox.claim_token::text, outbox.lease_until`,
		claimToken, now, leaseUntil,
	).Scan(
		&pending.Event.EventID, &pending.Event.OccurredAt, &pending.Event.Action,
		&pending.Event.UserID, &pending.Event.Email, &pending.Event.Resource,
		&pending.Event.ResourceID, &pending.Event.Detail, &pending.AttemptCount,
		&pending.ClaimToken, &pending.LeaseUntil,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim audit outbox event: %w", err)
	}
	pending.Event.OccurredAt = pending.Event.OccurredAt.UTC()
	pending.LeaseUntil = pending.LeaseUntil.UTC()
	return &pending, nil
}

// MarkAuditOutboxEventDelivered records successful delivery while retaining the event row.
func (d *DB) MarkAuditOutboxEventDelivered(ctx context.Context, eventID, claimToken string, completedAt time.Time) (bool, error) {
	if err := validateOutboxCompletion(eventID, claimToken, completedAt); err != nil {
		return false, fmt.Errorf("mark audit outbox event delivered: %w", err)
	}
	result, err := d.ExecContext(ctx, `
		UPDATE audit_outbox
		SET delivered_at = $3, claim_token = NULL, lease_until = NULL
		WHERE event_id = $1
		  AND claim_token = $2
		  AND $3 < lease_until
		  AND delivered_at IS NULL`,
		eventID, claimToken, completedAt,
	)
	if err != nil {
		return false, fmt.Errorf("mark audit outbox event delivered: %w", err)
	}
	return exactlyOneRowAffected(result)
}

// RescheduleAuditOutboxEvent releases a failed claim and sets its next due time.
func (d *DB) RescheduleAuditOutboxEvent(ctx context.Context, eventID, claimToken string, completedAt, nextAttemptAt time.Time) (bool, error) {
	if err := validateOutboxCompletion(eventID, claimToken, completedAt); err != nil {
		return false, fmt.Errorf("reschedule audit outbox event: %w", err)
	}
	if err := validateUTCTime("next_attempt_at", nextAttemptAt); err != nil {
		return false, fmt.Errorf("reschedule audit outbox event: %w", err)
	}
	if nextAttemptAt.Before(completedAt) {
		return false, fmt.Errorf("reschedule audit outbox event: next_attempt_at must not precede completed_at")
	}
	result, err := d.ExecContext(ctx, `
		UPDATE audit_outbox
		SET next_attempt_at = $4, claim_token = NULL, lease_until = NULL
		WHERE event_id = $1
		  AND claim_token = $2
		  AND $3 < lease_until
		  AND delivered_at IS NULL`,
		eventID, claimToken, completedAt, nextAttemptAt,
	)
	if err != nil {
		return false, fmt.Errorf("reschedule audit outbox event: %w", err)
	}
	return exactlyOneRowAffected(result)
}

func insertAuditOutboxEvent(ctx context.Context, tx *sql.Tx, event ext.AuditEvent) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO audit_outbox (
			event_id, occurred_at, action, user_id, email, resource, resource_id, detail, next_attempt_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $2)`,
		event.EventID, event.OccurredAt, event.Action, event.UserID, event.Email,
		event.Resource, event.ResourceID, event.Detail,
	)
	if err != nil {
		return fmt.Errorf("insert audit outbox event: %w", err)
	}
	return nil
}

func validateTokenAuditEvent(event ext.AuditEvent, action, tokenID, actor string, occurredAt time.Time) error {
	if !isCanonicalUUID(event.EventID) {
		return fmt.Errorf("event_id must be a canonical UUID")
	}
	if err := validateUTCTime("occurred_at", event.OccurredAt); err != nil {
		return err
	}
	if !event.OccurredAt.Equal(occurredAt) {
		return fmt.Errorf("occurred_at must match the token mutation time")
	}
	if event.Action != action {
		return fmt.Errorf("action must be %q", action)
	}
	if event.Resource != "opamp_token" {
		return fmt.Errorf("resource must be %q", "opamp_token")
	}
	if event.ResourceID != tokenID {
		return fmt.Errorf("resource_id must match the token ID")
	}
	if !isCanonicalOpAMPTokenID(event.ResourceID) {
		return fmt.Errorf("resource_id must be a canonical UUID")
	}
	if strings.TrimSpace(actor) == "" || event.UserID != actor {
		return fmt.Errorf("user_id must match the token mutation actor")
	}
	if event.Detail != "" {
		return fmt.Errorf("detail must be empty for token audit events")
	}
	fields := []struct {
		name  string
		value string
		max   int
	}{
		{name: "event_id", value: event.EventID, max: 128},
		{name: "action", value: event.Action, max: 128},
		{name: "user_id", value: event.UserID, max: 256},
		{name: "email", value: event.Email, max: 320},
		{name: "resource", value: event.Resource, max: 128},
		{name: "resource_id", value: event.ResourceID, max: 256},
		{name: "detail", value: event.Detail, max: 4096},
	}
	for _, field := range fields {
		if strings.ContainsAny(field.value, "\r\n") {
			return fmt.Errorf("%s must not contain newlines", field.name)
		}
		if len(field.value) > field.max {
			return fmt.Errorf("%s exceeds %d bytes", field.name, field.max)
		}
	}
	return nil
}

func validateOutboxCompletion(eventID, claimToken string, completedAt time.Time) error {
	if !isCanonicalUUID(eventID) {
		return fmt.Errorf("event_id must be a canonical UUID")
	}
	if !isCanonicalUUID(claimToken) {
		return fmt.Errorf("claim_token must be a canonical UUID")
	}
	return validateUTCTime("completed_at", completedAt)
}

func validateUTCTime(name string, value time.Time) error {
	if value.IsZero() {
		return fmt.Errorf("%s must not be zero", name)
	}
	if value.Location() != time.UTC {
		return fmt.Errorf("%s must use UTC", name)
	}
	return nil
}

func isCanonicalUUID(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.String() == value
}

func exactlyOneRowAffected(result sql.Result) (bool, error) {
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read affected rows: %w", err)
	}
	return rows == 1, nil
}
