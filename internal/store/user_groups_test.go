package store

import (
	"sort"
	"testing"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

func seedUser(t *testing.T, db *DB, id, email string) {
	t.Helper()
	if err := db.CreateUser(models.User{
		ID:           id,
		Email:        email,
		PasswordHash: "x",
	}); err != nil {
		t.Fatalf("seed user: %v", err)
	}
}

func TestAttachAndGetUserGroups(t *testing.T) {
	db := newTestDB(t)
	seedUser(t, db, "u1", "u1@example.com")

	if err := db.AttachUserToGroupByName("u1", "editor"); err != nil {
		t.Fatalf("Attach editor: %v", err)
	}
	if err := db.AttachUserToGroupByName("u1", "viewer"); err != nil {
		t.Fatalf("Attach viewer: %v", err)
	}

	groups, err := db.GetUserGroups("u1")
	if err != nil {
		t.Fatalf("GetUserGroups: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
	names := make([]string, 0, 2)
	for _, g := range groups {
		names = append(names, g.Name)
	}
	sort.Strings(names)
	if names[0] != "editor" || names[1] != "viewer" {
		t.Errorf("unexpected names: %v", names)
	}
}

func TestAttachUserToGroup_Idempotent(t *testing.T) {
	db := newTestDB(t)
	seedUser(t, db, "u1", "u1@example.com")
	if err := db.AttachUserToGroupByName("u1", "viewer"); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := db.AttachUserToGroupByName("u1", "viewer"); err != nil {
		t.Fatalf("second (idempotent): %v", err)
	}
	groups, _ := db.GetUserGroups("u1")
	if len(groups) != 1 {
		t.Errorf("expected 1 group after idempotent attach, got %d", len(groups))
	}
}

func TestGetUserGroups_UnknownUser(t *testing.T) {
	db := newTestDB(t)
	groups, err := db.GetUserGroups("ghost")
	if err != nil {
		t.Fatalf("GetUserGroups: %v", err)
	}
	if len(groups) != 0 {
		t.Errorf("expected empty slice for unknown user, got %d", len(groups))
	}
}
