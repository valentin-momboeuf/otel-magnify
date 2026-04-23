package bootstrap

import (
	"testing"

	"github.com/magnify-labs/otel-magnify/internal/store"
)

func TestSeedAdmin_AttachesAdministratorGroup(t *testing.T) {
	t.Setenv("SEED_ADMIN_EMAIL", "ops@example.com")
	t.Setenv("SEED_ADMIN_PASSWORD", "verylongpassword1234")

	db, err := store.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	seedAdmin(db)

	groups, err := db.GetUserGroups("admin-seed-001")
	if err != nil {
		t.Fatalf("GetUserGroups: %v", err)
	}
	if len(groups) != 1 || groups[0].Name != "administrator" {
		t.Fatalf("expected [administrator], got %v", groups)
	}
}
