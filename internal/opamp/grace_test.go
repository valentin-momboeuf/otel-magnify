package opamp

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestGraceFiresAfterDelayIfNotCancelled(t *testing.T) {
	gc := NewGraceController(20 * time.Millisecond)
	var fired int32
	gc.Schedule("wl", func() { atomic.AddInt32(&fired, 1) })
	time.Sleep(60 * time.Millisecond)
	if got := atomic.LoadInt32(&fired); got != 1 {
		t.Fatalf("expected 1 firing, got %d", got)
	}
}

func TestGraceCancellationPreventsFiring(t *testing.T) {
	gc := NewGraceController(30 * time.Millisecond)
	var fired int32
	gc.Schedule("wl", func() { atomic.AddInt32(&fired, 1) })
	gc.Cancel("wl")
	time.Sleep(60 * time.Millisecond)
	if got := atomic.LoadInt32(&fired); got != 0 {
		t.Fatalf("unexpected firing: %d", got)
	}
}

func TestGraceRescheduleReplacesExisting(t *testing.T) {
	gc := NewGraceController(30 * time.Millisecond)
	var fired int32
	gc.Schedule("wl", func() { atomic.AddInt32(&fired, 1) })
	gc.Schedule("wl", func() { atomic.AddInt32(&fired, 2) }) // should cancel the first
	time.Sleep(80 * time.Millisecond)
	if got := atomic.LoadInt32(&fired); got != 2 {
		t.Fatalf("expected 2 (second only), got %d", got)
	}
}

func TestGraceCancelOfUnknownIDIsNoop(_ *testing.T) {
	gc := NewGraceController(10 * time.Millisecond)
	gc.Cancel("wl-never-scheduled") // should not panic
}

func TestGraceMultipleWorkloadsIndependent(t *testing.T) {
	gc := NewGraceController(20 * time.Millisecond)
	var a, b int32
	gc.Schedule("wl-a", func() { atomic.AddInt32(&a, 1) })
	gc.Schedule("wl-b", func() { atomic.AddInt32(&b, 1) })
	gc.Cancel("wl-a")
	time.Sleep(60 * time.Millisecond)
	if atomic.LoadInt32(&a) != 0 {
		t.Fatalf("wl-a should have been cancelled, fired = %d", a)
	}
	if atomic.LoadInt32(&b) != 1 {
		t.Fatalf("wl-b should have fired once, got %d", b)
	}
}
