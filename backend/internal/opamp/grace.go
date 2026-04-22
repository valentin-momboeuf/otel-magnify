package opamp

import (
	"sync"
	"time"
)

// GraceController arms one delayed callback per key. Scheduling a new callback
// for a key cancels the previous one. Cancel removes a pending callback.
type GraceController struct {
	delay  time.Duration
	mu     sync.Mutex
	timers map[string]*time.Timer
}

// NewGraceController returns a controller with the given fixed delay.
func NewGraceController(delay time.Duration) *GraceController {
	return &GraceController{
		delay:  delay,
		timers: make(map[string]*time.Timer),
	}
}

// Schedule arms a timer for the given key. Any prior timer for the same key is
// cancelled. The callback runs in its own goroutine (time.AfterFunc semantics)
// after the configured delay.
func (g *GraceController) Schedule(key string, fn func()) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if t, ok := g.timers[key]; ok {
		t.Stop()
	}
	g.timers[key] = time.AfterFunc(g.delay, func() {
		g.mu.Lock()
		// Only delete if this is still the current timer (avoid deleting a
		// successor scheduled after AfterFunc started firing).
		delete(g.timers, key)
		g.mu.Unlock()
		fn()
	})
}

// Cancel stops and removes the pending timer for the key. No-op if none pending.
func (g *GraceController) Cancel(key string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if t, ok := g.timers[key]; ok {
		t.Stop()
		delete(g.timers, key)
	}
}
