package bootstrap_test

import (
	"context"
	"testing"
	"time"

	"github.com/magnify-labs/otel-magnify/pkg/bootstrap"
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
		if err != nil && err != context.Canceled {
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
