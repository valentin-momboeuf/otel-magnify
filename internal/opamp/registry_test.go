package opamp

import (
	"sync"
	"testing"
	"time"
)

func TestRegistryBindFreshReturnsTrueOnceThenFalse(t *testing.T) {
	r := NewInstanceRegistry()
	isFresh := r.BindInstance("uid-a", "wl-1", Instance{PodName: "pa", Version: "1.0", Healthy: true, ConnectedAt: time.Now().UTC()})
	if !isFresh {
		t.Fatalf("first bind should be fresh")
	}
	if wl, ok := r.LookupWorkload("uid-a"); !ok || wl != "wl-1" {
		t.Fatalf("lookup: %q %v", wl, ok)
	}
	// Re-bind same uid → not fresh
	isFresh = r.BindInstance("uid-a", "wl-1", Instance{PodName: "pa", Version: "1.0"})
	if isFresh {
		t.Fatalf("re-bind should not be fresh")
	}
}

func TestRegistryUnbindReturnsWorkloadAndShrinksCount(t *testing.T) {
	r := NewInstanceRegistry()
	r.BindInstance("uid-a", "wl-1", Instance{})
	r.BindInstance("uid-b", "wl-1", Instance{})
	if r.Count("wl-1") != 2 {
		t.Fatalf("count: %d", r.Count("wl-1"))
	}
	wl := r.UnbindInstance("uid-a")
	if wl != "wl-1" {
		t.Fatalf("unbind returned %q", wl)
	}
	if r.Count("wl-1") != 1 {
		t.Fatalf("count after unbind: %d", r.Count("wl-1"))
	}
	r.UnbindInstance("uid-b")
	if r.Count("wl-1") != 0 {
		t.Fatalf("count: %d", r.Count("wl-1"))
	}
	// Unbind of unknown uid returns empty string
	if r.UnbindInstance("uid-missing") != "" {
		t.Fatal("unknown unbind should return empty string")
	}
}

func TestRegistryInstancesSnapshot(t *testing.T) {
	r := NewInstanceRegistry()
	r.BindInstance("uid-a", "wl-1", Instance{PodName: "pa", Version: "1.0"})
	r.BindInstance("uid-b", "wl-1", Instance{PodName: "pb", Version: "1.1"})
	snap := r.Instances("wl-1")
	if len(snap) != 2 {
		t.Fatalf("snap: %d", len(snap))
	}
}

func TestRegistryUpdateInstance(t *testing.T) {
	r := NewInstanceRegistry()
	r.BindInstance("uid-a", "wl-1", Instance{Version: "1.0", Healthy: true})
	ok := r.UpdateInstance("uid-a", func(i *Instance) { i.Healthy = false })
	if !ok {
		t.Fatal("UpdateInstance should return true for known uid")
	}
	snap := r.Instances("wl-1")
	if len(snap) != 1 || snap[0].Healthy {
		t.Fatalf("expected unhealthy, got %+v", snap)
	}
	if ok := r.UpdateInstance("uid-missing", func(i *Instance) {}); ok {
		t.Fatal("UpdateInstance should return false for unknown uid")
	}
}

func TestRegistryAggregatedStatus(t *testing.T) {
	r := NewInstanceRegistry()
	if s := r.AggregatedStatus("wl-1"); s != "disconnected" {
		t.Fatalf("empty workload: got %q, want disconnected", s)
	}
	r.BindInstance("uid-a", "wl-1", Instance{Healthy: true})
	if s := r.AggregatedStatus("wl-1"); s != "connected" {
		t.Fatalf("one healthy: got %q, want connected", s)
	}
	r.BindInstance("uid-b", "wl-1", Instance{Healthy: false})
	if s := r.AggregatedStatus("wl-1"); s != "degraded" {
		t.Fatalf("one unhealthy among two: got %q, want degraded", s)
	}
}

func TestRegistryPreviousVersion(t *testing.T) {
	r := NewInstanceRegistry()
	_, ok := r.PreviousVersion("uid-a")
	if ok {
		t.Fatal("expected not-found for unknown uid")
	}
	r.BindInstance("uid-a", "wl-1", Instance{Version: "1.0"})
	prev, ok := r.PreviousVersion("uid-a")
	if !ok || prev != "1.0" {
		t.Fatalf("PreviousVersion = %q, %v", prev, ok)
	}
}

func TestRegistryConcurrentBindUnbind(t *testing.T) {
	r := NewInstanceRegistry()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)
		uid := string(rune('a' + (i % 26)))
		go func() {
			defer wg.Done()
			r.BindInstance(uid, "wl-1", Instance{})
		}()
		go func() {
			defer wg.Done()
			_ = r.Count("wl-1")
			_ = r.Instances("wl-1")
			_, _ = r.LookupWorkload(uid)
			r.UnbindInstance(uid)
		}()
	}
	wg.Wait()
	// Just ensure no crash / race — run with -race to surface issues.
}
