package opamp

import (
	"sync"
	"time"
)

// GraceController arms one delayed callback per key. Scheduling a new callback
// for a key cancels the previous one. Cancel removes a pending callback.
type GraceController struct {
	delay   time.Duration
	mu      sync.Mutex
	timers  map[string]*graceTimer
	stopped bool
	nextID  uint64
	wg      sync.WaitGroup
}

type graceTimer struct {
	timer *time.Timer
	id    uint64
	done  sync.Once
}

func (t *graceTimer) finish(wg *sync.WaitGroup) {
	t.done.Do(wg.Done)
}

// NewGraceController returns a controller with the given fixed delay.
func NewGraceController(delay time.Duration) *GraceController {
	return &GraceController{
		delay:  delay,
		timers: make(map[string]*graceTimer),
	}
}

// Schedule arms a timer for the given key. Any prior timer for the same key is
// cancelled. The callback runs in its own goroutine (time.AfterFunc semantics)
// after the configured delay.
func (g *GraceController) Schedule(key string, fn func()) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.stopped {
		return
	}
	if previous, ok := g.timers[key]; ok {
		delete(g.timers, key)
		if previous.timer.Stop() {
			previous.finish(&g.wg)
		}
	}

	g.nextID++
	entry := &graceTimer{id: g.nextID}
	g.wg.Add(1)
	entry.timer = time.AfterFunc(g.delay, func() {
		g.mu.Lock()
		current, currentExists := g.timers[key]
		if g.stopped || !currentExists || current != entry || current.id != entry.id {
			g.mu.Unlock()
			entry.finish(&g.wg)
			return
		}
		delete(g.timers, key)
		g.mu.Unlock()
		defer entry.finish(&g.wg)
		fn()
	})
	g.timers[key] = entry
}

// Cancel stops and removes the pending timer for the key. No-op if none pending.
func (g *GraceController) Cancel(key string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if entry, ok := g.timers[key]; ok {
		delete(g.timers, key)
		if entry.timer.Stop() {
			entry.finish(&g.wg)
		}
	}
}

// Stop prevents future schedules, cancels pending callbacks, and waits for any
// callback that already entered before shutdown.
func (g *GraceController) Stop() {
	g.mu.Lock()
	if !g.stopped {
		g.stopped = true
		for key, entry := range g.timers {
			delete(g.timers, key)
			if entry.timer.Stop() {
				entry.finish(&g.wg)
			}
		}
	}
	g.mu.Unlock()
	g.wg.Wait()
}
