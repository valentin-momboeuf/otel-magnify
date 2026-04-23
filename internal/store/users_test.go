package store

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/magnify-labs/otel-magnify/pkg/models"

	"golang.org/x/crypto/bcrypt"
)

func TestCreateUser(t *testing.T) {
	db := newTestDB(t)

	hash, _ := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.DefaultCost)
	user := models.User{
		ID:           "user-001",
		Email:        "admin@test.com",
		PasswordHash: string(hash),
	}

	if err := db.CreateUser(user); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	got, err := db.GetUserByEmail("admin@test.com")
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	if bcrypt.CompareHashAndPassword([]byte(got.PasswordHash), []byte("secret")) != nil {
		t.Error("password hash mismatch")
	}
}

func TestGetUserByEmail_NotFound(t *testing.T) {
	db := newTestDB(t)

	_, err := db.GetUserByEmail("nobody@test.com")
	if err == nil {
		t.Error("expected error for non-existent user")
	}
}

func TestUpdateUser_UpdatesFields(t *testing.T) {
	db := newTestDB(t)

	hash, _ := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.DefaultCost)
	original := models.User{
		ID:           "user-001",
		Email:        "alice@test.com",
		PasswordHash: string(hash),
	}
	if err := db.CreateUser(original); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	if err := db.UpdateUser(original); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}

	got, err := db.GetUserByEmail("alice@test.com")
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	// PasswordHash must be preserved when the caller passes it unchanged.
	if got.PasswordHash != string(hash) {
		t.Error("PasswordHash was unexpectedly modified")
	}
}

func TestUpdateUser_NotFound(t *testing.T) {
	db := newTestDB(t)

	err := db.UpdateUser(models.User{
		ID:    "ghost-999",
		Email: "ghost@test.com",
	})
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("err = %v, want sql.ErrNoRows", err)
	}
}
