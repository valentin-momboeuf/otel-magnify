package opamp

import (
	"testing"
	"time"
)

func TestGraceFiresAfterDelayIfNotCancelled(t *testing.T) {
	t.Parallel()

	gc := NewGraceController(20 * time.Millisecond)
	fired := make(chan struct{}, 1)
	gc.Schedule("wl", func() { fired <- struct{}{} })
	select {
	case <-fired:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected grace callback to fire")
	}
}

func TestGraceCancellationPreventsFiring(t *testing.T) {
	t.Parallel()

	gc := NewGraceController(30 * time.Millisecond)
	fired := make(chan struct{}, 1)
	gc.Schedule("wl", func() { fired <- struct{}{} })
	gc.Cancel("wl")
	select {
	case <-fired:
		t.Fatal("unexpected grace callback after cancel")
	case <-time.After(70 * time.Millisecond):
	}
}

func TestGraceRescheduleReplacesExisting(t *testing.T) {
	t.Parallel()

	gc := NewGraceController(30 * time.Millisecond)
	fired := make(chan int, 2)
	gc.Schedule("wl", func() { fired <- 1 })
	gc.Schedule("wl", func() { fired <- 2 }) // should cancel the first
	select {
	case got := <-fired:
		if got != 2 {
			t.Fatalf("expected 2 (second only), got %d", got)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected rescheduled grace callback to fire")
	}
	select {
	case got := <-fired:
		t.Fatalf("expected 2 (second only), got %d", got)
	case <-time.After(70 * time.Millisecond):
	}
}

func TestGraceCancelOfUnknownIDIsNoop(_ *testing.T) {
	gc := NewGraceController(10 * time.Millisecond)
	gc.Cancel("wl-never-scheduled") // should not panic
}

func TestGraceMultipleWorkloadsIndependent(t *testing.T) {
	t.Parallel()

	gc := NewGraceController(20 * time.Millisecond)
	a := make(chan struct{}, 1)
	b := make(chan struct{}, 1)
	gc.Schedule("wl-a", func() { a <- struct{}{} })
	gc.Schedule("wl-b", func() { b <- struct{}{} })
	gc.Cancel("wl-a")
	select {
	case <-b:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("wl-b should have fired once")
	}
	select {
	case <-a:
		t.Fatal("wl-a should have been cancelled")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestGraceStopCancelsPendingAndRejectsFutureSchedules(t *testing.T) {
	gc := NewGraceController(30 * time.Millisecond)
	fired := make(chan string, 2)
	gc.Schedule("pending", func() { fired <- "pending" })

	gc.Stop()
	gc.Stop()
	gc.Schedule("after-stop", func() { fired <- "after-stop" })

	select {
	case name := <-fired:
		t.Fatalf("grace callback %q fired after Stop", name)
	case <-time.After(70 * time.Millisecond):
	}
}

func TestGraceStopWaitsForCallbackAlreadyInFlight(t *testing.T) {
	gc := NewGraceController(time.Millisecond)
	started := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	gc.Schedule("in-flight", func() {
		close(started)
		<-release
		close(finished)
	})
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("grace callback did not enter")
	}

	stopped := make(chan struct{})
	go func() {
		gc.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
		t.Fatal("Stop returned before in-flight callback finished")
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("grace callback did not finish")
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("Stop did not return after in-flight callback finished")
	}
}
