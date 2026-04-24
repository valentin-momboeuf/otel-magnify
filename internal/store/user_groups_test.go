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

func TestDetachUserFromGroup_Idempotent(t *testing.T) {
	db := newTestDB(t)
	seedUser(t, db, "u1", "a@b.c")
	if err := db.AttachUserToGroupByName("u1", "viewer"); err != nil {
		t.Fatalf("attach: %v", err)
	}

	// First detach: removes the membership.
	if err := db.DetachUserFromGroup("u1", "viewer"); err != nil {
		t.Fatalf("first detach: %v", err)
	}
	groups, err := db.GetUserGroups("u1")
	if err != nil {
		t.Fatalf("GetUserGroups: %v", err)
	}
	if len(groups) != 0 {
		t.Fatalf("expected 0 groups after detach, got %d", len(groups))
	}

	// Second detach: idempotent no-op, no error.
	if err := db.DetachUserFromGroup("u1", "viewer"); err != nil {
		t.Fatalf("second detach should be no-op, got: %v", err)
	}
}

func TestDetachUserFromGroup_UnknownGroup_ReturnsError(t *testing.T) {
	db := newTestDB(t)
	seedUser(t, db, "u1", "a@b.c")

	err := db.DetachUserFromGroup("u1", "does-not-exist")
	if err == nil {
		t.Fatal("expected error for unknown group, got nil")
	}
}

func TestDetachUserFromGroup_UnknownUser_ReturnsError(t *testing.T) {
	db := newTestDB(t)
	err := db.DetachUserFromGroup("ghost", "viewer")
	if err == nil {
		t.Fatal("expected error for unknown user, got nil")
	}
}

func TestReplaceUserGroups_ReplacesExactly(t *testing.T) {
	db := newTestDB(t)
	seedUser(t, db, "u1", "a@b.c")
	if err := db.AttachUserToGroupByName("u1", "viewer"); err != nil {
		t.Fatalf("attach viewer: %v", err)
	}
	if err := db.AttachUserToGroupByName("u1", "editor"); err != nil {
		t.Fatalf("attach editor: %v", err)
	}

	if err := db.ReplaceUserGroups("u1", []string{"administrator"}); err != nil {
		t.Fatalf("ReplaceUserGroups: %v", err)
	}
	groups, err := db.GetUserGroups("u1")
	if err != nil {
		t.Fatalf("GetUserGroups: %v", err)
	}
	if len(groups) != 1 || groups[0].Name != "administrator" {
		names := make([]string, len(groups))
		for i, g := range groups {
			names[i] = g.Name
		}
		t.Fatalf("expected exactly [administrator], got %v", names)
	}
}

func TestReplaceUserGroups_EmptyReplacement_RemovesAll(t *testing.T) {
	db := newTestDB(t)
	seedUser(t, db, "u1", "a@b.c")
	if err := db.AttachUserToGroupByName("u1", "viewer"); err != nil {
		t.Fatalf("attach viewer: %v", err)
	}
	if err := db.AttachUserToGroupByName("u1", "editor"); err != nil {
		t.Fatalf("attach editor: %v", err)
	}

	if err := db.ReplaceUserGroups("u1", nil); err != nil {
		t.Fatalf("ReplaceUserGroups(nil): %v", err)
	}
	groups, err := db.GetUserGroups("u1")
	if err != nil {
		t.Fatalf("GetUserGroups: %v", err)
	}
	if len(groups) != 0 {
		t.Fatalf("expected 0 groups, got %d", len(groups))
	}
}

func TestReplaceUserGroups_UnknownGroupName_RollsBack(t *testing.T) {
	db := newTestDB(t)
	seedUser(t, db, "u1", "a@b.c")
	if err := db.AttachUserToGroupByName("u1", "viewer"); err != nil {
		t.Fatalf("attach viewer: %v", err)
	}

	err := db.ReplaceUserGroups("u1", []string{"editor", "does-not-exist"})
	if err == nil {
		t.Fatal("expected error for unknown group name")
	}
	// Rollback: user must still be in `viewer` only — no partial write.
	groups, err := db.GetUserGroups("u1")
	if err != nil {
		t.Fatalf("GetUserGroups: %v", err)
	}
	if len(groups) != 1 || groups[0].Name != "viewer" {
		names := make([]string, len(groups))
		for i, g := range groups {
			names[i] = g.Name
		}
		t.Fatalf("expected [viewer] after rollback, got %v", names)
	}
}

func TestReplaceUserGroups_UnknownUser_ReturnsError(t *testing.T) {
	db := newTestDB(t)
	err := db.ReplaceUserGroups("ghost", []string{"viewer"})
	if err == nil {
		t.Fatal("expected error for unknown user")
	}
}
