package store

import (
	"testing"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

func TestGetUserPreferences_DefaultsWhenMissing(t *testing.T) {
	db := newTestDB(t)
	seedUser(t, db, "u1", "u1@example.com")
	prefs, err := db.GetUserPreferences("u1")
	if err != nil {
		t.Fatalf("GetUserPreferences: %v", err)
	}
	if prefs.Theme != "system" || prefs.Language != "en" {
		t.Errorf("expected defaults (system/en), got %+v", prefs)
	}
}

func TestUpsertUserPreferences_CreatesThenUpdates(t *testing.T) {
	db := newTestDB(t)
	seedUser(t, db, "u1", "u1@example.com")

	if err := db.UpsertUserPreferences(models.UserPreferences{
		UserID: "u1", Theme: "dark", Language: "fr",
	}); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	got, _ := db.GetUserPreferences("u1")
	if got.Theme != "dark" || got.Language != "fr" {
		t.Errorf("after create, got %+v", got)
	}

	if err := db.UpsertUserPreferences(models.UserPreferences{
		UserID: "u1", Theme: "light", Language: "en",
	}); err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	got, _ = db.GetUserPreferences("u1")
	if got.Theme != "light" || got.Language != "en" {
		t.Errorf("after update, got %+v", got)
	}
}

func TestUpsertUserPreferences_RejectsInvalidTheme(t *testing.T) {
	db := newTestDB(t)
	seedUser(t, db, "u1", "u1@example.com")
	err := db.UpsertUserPreferences(models.UserPreferences{
		UserID: "u1", Theme: "neon", Language: "en",
	})
	if err == nil {
		t.Fatal("expected error on invalid theme, got nil")
	}
}
