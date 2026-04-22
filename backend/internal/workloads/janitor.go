package workloads

import (
	"context"
	"log"
	"time"
)

type Store interface {
	ArchiveExpiredWorkloads(now time.Time) (int64, error)
	PurgeOldWorkloadEvents(cutoff time.Time) (int64, error)
}

type Options struct {
	Interval       time.Duration
	EventRetention time.Duration
}

type Janitor struct {
	store Store
	opts  Options
}

// New returns a janitor that archives expired workloads and purges old events
// on its Start tick.
func New(store Store, opts Options) *Janitor {
	if opts.Interval <= 0 {
		opts.Interval = 5 * time.Minute
	}
	if opts.EventRetention <= 0 {
		opts.EventRetention = 30 * 24 * time.Hour
	}
	return &Janitor{store: store, opts: opts}
}

// Start runs the janitor loop until ctx is cancelled.
func (j *Janitor) Start(ctx context.Context) {
	ticker := time.NewTicker(j.opts.Interval)
	defer ticker.Stop()
	for {
		select {
		case now := <-ticker.C:
			j.RunOnce(ctx, now)
		case <-ctx.Done():
			return
		}
	}
}

// RunOnce performs one archive + purge pass. Exposed for tests and manual runs.
func (j *Janitor) RunOnce(_ context.Context, now time.Time) {
	if n, err := j.store.ArchiveExpiredWorkloads(now); err != nil {
		log.Printf("janitor archive: %v", err)
	} else if n > 0 {
		log.Printf("janitor: archived %d workload(s)", n)
	}
	cutoff := now.Add(-j.opts.EventRetention)
	if n, err := j.store.PurgeOldWorkloadEvents(cutoff); err != nil {
		log.Printf("janitor purge events: %v", err)
	} else if n > 0 {
		log.Printf("janitor: purged %d event(s) older than %s", n, cutoff.Format(time.RFC3339))
	}
}
