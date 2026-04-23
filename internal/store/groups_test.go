package store

import (
	"testing"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

func TestListSystemGroups_ReturnsThreeSeededRows(t *testing.T) {
	db := newTestDB(t)
	groups, err := db.ListSystemGroups()
	if err != nil {
		t.Fatalf("ListSystemGroups: %v", err)
	}
	if len(groups) != 3 {
		t.Fatalf("expected 3 system groups, got %d", len(groups))
	}
	names := map[string]bool{}
	for _, g := range groups {
		if !g.IsSystem {
			t.Errorf("group %q should have IsSystem=true", g.Name)
		}
		names[g.Name] = true
	}
	for _, want := range []string{"viewer", "editor", "administrator"} {
		if !names[want] {
			t.Errorf("missing system group %q", want)
		}
	}
}

func TestGetGroupByName(t *testing.T) {
	db := newTestDB(t)
	g, err := db.GetGroupByName("administrator")
	if err != nil {
		t.Fatalf("GetGroupByName: %v", err)
	}
	if g.ID != "grp_system_administrator" || g.Role != "administrator" {
		t.Errorf("unexpected group: %+v", g)
	}
}

func TestGetGroupByName_NotFound(t *testing.T) {
	db := newTestDB(t)
	if _, err := db.GetGroupByName("nope"); err == nil {
		t.Fatal("expected error for unknown group")
	}
}

// guard type-check: the return type must be the models.Group slice.
var _ = func() []models.Group { return nil }
