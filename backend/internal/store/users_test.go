package store

import (
	"testing"

	"otel-magnify/pkg/models"

	"golang.org/x/crypto/bcrypt"
)

func TestCreateUser(t *testing.T) {
	db := newTestDB(t)

	hash, _ := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.DefaultCost)
	user := models.User{
		ID:           "user-001",
		Email:        "admin@test.com",
		PasswordHash: string(hash),
		Role:         "admin",
	}

	if err := db.CreateUser(user); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	got, err := db.GetUserByEmail("admin@test.com")
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	if got.Role != "admin" {
		t.Errorf("Role = %q, want admin", got.Role)
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
