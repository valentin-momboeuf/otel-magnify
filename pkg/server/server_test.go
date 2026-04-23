package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/magnify-labs/otel-magnify/internal/auth"
	"github.com/magnify-labs/otel-magnify/internal/store"
	"github.com/magnify-labs/otel-magnify/pkg/ext"
	"github.com/magnify-labs/otel-magnify/pkg/server"
)

func TestNew_DefaultsCompile(t *testing.T) {
	db, err := store.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}

	a := auth.New("test-secret-key-at-least-32-bytes!")

	srv := server.New(server.Config{
		ListenAddr: ":0",
		OpAMPAddr:  ":0",
	}, db, a)

	if srv == nil {
		t.Fatal("New returned nil")
	}
}

func TestServer_StartsAndStops(t *testing.T) {
	db, err := store.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}

	a := auth.New("test-secret-key-at-least-32-bytes!")

	srv := server.New(server.Config{
		ListenAddr: ":0",
		OpAMPAddr:  ":0",
	}, db, a)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Run(ctx) }()

	// Give server time to start
	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err != nil && err != context.Canceled {
			t.Fatalf("Run returned unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Server did not stop within 5 seconds")
	}
}

func TestAuthMethods_DefaultsToPassword(t *testing.T) {
	db, err := store.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}

	a := auth.New("test-secret-key-at-least-32-bytes!")
	srv := server.New(server.Config{ListenAddr: ":0", OpAMPAddr: ":0"}, db, a)

	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/auth/methods", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Methods []ext.AuthMethod `json:"methods"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(body.Methods) != 1 {
		t.Fatalf("len(methods) = %d, want 1", len(body.Methods))
	}
	got := body.Methods[0]
	if got.ID != "password" || got.Type != "password" {
		t.Errorf("method = %+v, want id=password type=password", got)
	}
	if got.LoginURL != "/api/auth/login" {
		t.Errorf("LoginURL = %q, want /api/auth/login", got.LoginURL)
	}
}

func TestAuthMethods_AppendsViaOption(t *testing.T) {
	db, err := store.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}

	a := auth.New("test-secret-key-at-least-32-bytes!")
	okta := ext.AuthMethod{
		ID:          "okta-main",
		Type:        "sso",
		DisplayName: "Okta",
		LoginURL:    "/api/auth/sso/okta-main/login",
	}

	srv := server.New(server.Config{ListenAddr: ":0", OpAMPAddr: ":0"}, db, a,
		server.WithAuthMethod(okta),
	)

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/auth/methods", nil))

	var body struct {
		Methods []ext.AuthMethod `json:"methods"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(body.Methods) != 2 {
		t.Fatalf("len(methods) = %d, want 2", len(body.Methods))
	}
	if body.Methods[0].ID != "password" {
		t.Errorf("methods[0].ID = %q, want password (default must come first)", body.Methods[0].ID)
	}
	if body.Methods[1] != okta {
		t.Errorf("methods[1] = %+v, want %+v", body.Methods[1], okta)
	}
}

func TestAuthMethods_DuplicateIDKeepsFirst(t *testing.T) {
	db, err := store.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}

	a := auth.New("test-secret-key-at-least-32-bytes!")

	srv := server.New(server.Config{ListenAddr: ":0", OpAMPAddr: ":0"}, db, a,
		server.WithAuthMethod(ext.AuthMethod{
			ID: "password", Type: "sso", DisplayName: "Override", LoginURL: "/nope",
		}),
	)

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/auth/methods", nil))

	var body struct {
		Methods []ext.AuthMethod `json:"methods"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(body.Methods) != 1 {
		t.Fatalf("len(methods) = %d, want 1 (duplicate must be dropped)", len(body.Methods))
	}
	if body.Methods[0].DisplayName != "Email + password" {
		t.Errorf("default method was overridden: got DisplayName=%q", body.Methods[0].DisplayName)
	}
}
