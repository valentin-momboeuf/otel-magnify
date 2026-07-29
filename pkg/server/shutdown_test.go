package server

import (
	"context"
	"sync"
	"testing"
	"time"
)

type joinableOpAMPStopper struct {
	mu            sync.Mutex
	calls         int
	firstCalled   chan struct{}
	secondCalled  chan struct{}
	secondRelease chan struct{}
}

func (s *joinableOpAMPStopper) Stop(ctx context.Context) error {
	s.mu.Lock()
	s.calls++
	call := s.calls
	s.mu.Unlock()
	if call == 1 {
		close(s.firstCalled)
		<-ctx.Done()
		return ctx.Err()
	}
	close(s.secondCalled)
	<-s.secondRelease
	return nil
}

func TestStopOpAMPServerJoinsCleanupAfterContextTimeout(t *testing.T) {
	stopper := &joinableOpAMPStopper{
		firstCalled:   make(chan struct{}),
		secondCalled:  make(chan struct{}),
		secondRelease: make(chan struct{}),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	go func() {
		stopOpAMPServer(ctx, stopper)
		close(done)
	}()
	select {
	case <-stopper.firstCalled:
	case <-time.After(time.Second):
		t.Fatal("bounded OpAMP Stop did not start")
	}
	select {
	case <-stopper.secondCalled:
	case <-time.After(time.Second):
		t.Fatal("OpAMP Stop timeout was not followed by a background join")
	}
	select {
	case <-done:
		close(stopper.secondRelease)
		t.Fatal("shutdown returned before the background OpAMP cleanup completed")
	case <-time.After(30 * time.Millisecond):
	}
	close(stopper.secondRelease)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not finish after the background OpAMP cleanup")
	}
	stopper.mu.Lock()
	calls := stopper.calls
	stopper.mu.Unlock()
	if calls != 2 {
		t.Fatalf("OpAMP Stop calls = %d, want bounded call plus one join", calls)
	}
}
