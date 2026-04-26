package bootstrap_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/magnify-labs/otel-magnify/pkg/bootstrap"
	"github.com/magnify-labs/otel-magnify/pkg/ext"
	"github.com/magnify-labs/otel-magnify/pkg/server"
)

// TestRun_ReturnsOnContextCancel confirms that bootstrap.Run honours
// context cancellation and returns cleanly. It runs with a minimal
// in-memory SQLite store and a short-lived context.
func TestRun_ReturnsOnContextCancel(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key-at-least-32-bytes!")
	t.Setenv("DB_DRIVER", "sqlite")
	t.Setenv("DB_DSN", ":memory:")
	t.Setenv("LISTEN_ADDR", ":0")
	t.Setenv("OPAMP_ADDR", ":0")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Run must return regardless of where it is in startup.

	errCh := make(chan error, 1)
	go func() { errCh <- bootstrap.Run(ctx, bootstrap.Options{}) }()

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("Run returned unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return within 5 seconds of cancel")
	}
}

// TestRun_FailsWithoutJWTSecret confirms that Run surfaces a missing
// JWT_SECRET as an error rather than calling os.Exit.
func TestRun_FailsWithoutJWTSecret(t *testing.T) {
	t.Setenv("JWT_SECRET", "")
	t.Setenv("DB_DRIVER", "sqlite")
	t.Setenv("DB_DSN", ":memory:")

	err := bootstrap.Run(context.Background(), bootstrap.Options{})
	if err == nil {
		t.Fatal("expected error when JWT_SECRET is unset, got nil")
	}
}

func TestPreRun_CalledAfterMigrations_BeforeServerStart(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-at-least-32-bytes-long-for-hmac")
	t.Setenv("DB_DRIVER", "sqlite")
	t.Setenv("DB_DSN", ":memory:")
	t.Setenv("LISTEN_ADDR", ":0")
	t.Setenv("OPAMP_ADDR", ":0")

	gotGroups := false
	gotAuth := false
	opts := bootstrap.Options{
		PreRun: func(store ext.Store, auth ext.AuthProvider) ([]server.Option, error) {
			// Migrations already applied: seeded groups must exist.
			groups, err := store.ListSystemGroups()
			if err != nil {
				return nil, fmt.Errorf("ListSystemGroups in PreRun: %w", err)
			}
			if len(groups) >= 3 {
				gotGroups = true
			}
			// Auth provider must be non-nil and functional: minting should work.
			if auth != nil {
				if _, err := auth.GenerateToken("u1", "e@x", []string{"viewer"}); err == nil {
					gotAuth = true
				}
			}
			return nil, nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- bootstrap.Run(ctx, opts) }()

	time.Sleep(500 * time.Millisecond)
	cancel()
	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("bootstrap.Run: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("bootstrap.Run did not return after cancel")
	}

	if !gotGroups {
		t.Fatal("PreRun did not see seeded system groups — ran before migrations?")
	}
	if !gotAuth {
		t.Fatal("PreRun did not receive a functional auth provider")
	}
}

// NOTE: coverage of "PreRun-returned server.Option is applied to the
// server" is provided transitively by TestPreRun_CalledAfterMigrations_BeforeServerStart
// (PreRun is called with the right args) and by TestAuthMethodProvider_*
// in pkg/server (the option, once registered, is consulted by the
// /api/auth/methods handler).

func TestPreRun_Error_PropagatesAsRunError(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-at-least-32-bytes-long-for-hmac")
	t.Setenv("DB_DRIVER", "sqlite")
	t.Setenv("DB_DSN", ":memory:")
	t.Setenv("LISTEN_ADDR", ":0")
	t.Setenv("OPAMP_ADDR", ":0")

	want := errors.New("prerun boom")
	opts := bootstrap.Options{
		PreRun: func(store ext.Store, auth ext.AuthProvider) ([]server.Option, error) {
			return nil, want
		},
	}
	err := bootstrap.Run(context.Background(), opts)
	if !errors.Is(err, want) {
		t.Fatalf("expected PreRun error to propagate, got %v", err)
	}
}
