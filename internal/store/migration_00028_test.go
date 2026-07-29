package store

import (
	"strings"
	"testing"
	"time"
)

func TestAuditOutboxMigration00028CreatesDurableSchema(t *testing.T) {
	db := newTestDB(t)

	expectedTypes := map[string]string{
		"event_id":        "text",
		"occurred_at":     "timestamp with time zone",
		"action":          "text",
		"user_id":         "text",
		"email":           "text",
		"resource":        "text",
		"resource_id":     "text",
		"detail":          "text",
		"attempt_count":   "integer",
		"next_attempt_at": "timestamp with time zone",
		"claim_token":     "uuid",
		"lease_until":     "timestamp with time zone",
		"delivered_at":    "timestamp with time zone",
	}
	for column, wantType := range expectedTypes {
		var gotType string
		if err := db.QueryRow(`
			SELECT data_type
			FROM information_schema.columns
			WHERE table_schema = current_schema()
			  AND table_name = 'audit_outbox'
			  AND column_name = ?`, column,
		).Scan(&gotType); err != nil {
			t.Fatalf("query audit_outbox.%s type: %v", column, err)
		}
		if gotType != wantType {
			t.Errorf("audit_outbox.%s type = %q, want %q", column, gotType, wantType)
		}
	}

	var indexDefinition string
	if err := db.QueryRow(`
		SELECT indexdef
		FROM pg_indexes
		WHERE schemaname = current_schema()
		  AND indexname = 'idx_audit_outbox_pending'`,
	).Scan(&indexDefinition); err != nil {
		t.Fatalf("query pending index: %v", err)
	}
	for _, fragment := range []string{"(next_attempt_at, occurred_at, event_id)", "WHERE (delivered_at IS NULL)"} {
		if !strings.Contains(indexDefinition, fragment) {
			t.Errorf("pending index = %q, want fragment %q", indexDefinition, fragment)
		}
	}
	assertGooseVersion(t, db, 28)
}

func TestAuditOutboxMigration00028EnforcesLeaseAndDeliveryState(t *testing.T) {
	db := newTestDB(t)
	now := time.Date(2026, time.July, 22, 10, 0, 0, 0, time.UTC)

	insert := func(eventID string, claimToken any, leaseUntil any, deliveredAt any) error {
		_, err := db.Exec(`
			INSERT INTO audit_outbox (
				event_id, occurred_at, action, user_id, email, resource,
				resource_id, detail, next_attempt_at, claim_token, lease_until, delivered_at
			) VALUES (?, ?, ?, '', '', ?, '', '', ?, ?, ?, ?)`,
			eventID, now, "opamp.token.create", "opamp_token", now, claimToken, leaseUntil, deliveredAt,
		)
		return err
	}

	if err := insert("00000000-0000-4000-8000-000000000101", nil, nil, nil); err != nil {
		t.Fatalf("insert pending event: %v", err)
	}
	if err := insert("00000000-0000-4000-8000-000000000102", "10000000-0000-4000-8000-000000000001", now.Add(time.Minute), nil); err != nil {
		t.Fatalf("insert claimed event: %v", err)
	}
	if err := insert("00000000-0000-4000-8000-000000000103", nil, nil, now); err != nil {
		t.Fatalf("insert delivered event: %v", err)
	}

	invalid := []struct {
		name        string
		eventID     string
		claimToken  any
		leaseUntil  any
		deliveredAt any
	}{
		{name: "claim_without_lease", eventID: "00000000-0000-4000-8000-000000000111", claimToken: "10000000-0000-4000-8000-000000000011"},
		{name: "lease_without_claim", eventID: "00000000-0000-4000-8000-000000000112", leaseUntil: now.Add(time.Minute)},
		{name: "delivered_while_claimed", eventID: "00000000-0000-4000-8000-000000000113", claimToken: "10000000-0000-4000-8000-000000000013", leaseUntil: now.Add(time.Minute), deliveredAt: now},
	}
	for _, tt := range invalid {
		t.Run(tt.name, func(t *testing.T) {
			if err := insert(tt.eventID, tt.claimToken, tt.leaseUntil, tt.deliveredAt); err == nil {
				t.Fatal("INSERT error = nil, want constraint violation")
			}
		})
	}
}
