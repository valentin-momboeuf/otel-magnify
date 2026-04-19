package server_test

import (
	"context"
	"testing"
	"time"

	"github.com/magnify-labs/otel-magnify/internal/auth"
	"github.com/magnify-labs/otel-magnify/internal/store"
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
