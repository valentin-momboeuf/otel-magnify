package opamp

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/open-telemetry/opamp-go/protobufs"
	"github.com/open-telemetry/opamp-go/server/types"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

type tokenConnectionTestClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *tokenConnectionTestClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *tokenConnectionTestClock) Set(now time.Time) {
	c.mu.Lock()
	c.now = now
	c.mu.Unlock()
}

type tokenConnectionTestTimer struct {
	mu      sync.Mutex
	stopped bool
	fn      func()
}

func (t *tokenConnectionTestTimer) Stop() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	wasActive := !t.stopped
	t.stopped = true
	return wasActive
}

// Fire intentionally invokes callbacks even after Stop. This models a timer
// callback that was already queued when Stop raced with it.
func (t *tokenConnectionTestTimer) Fire() {
	t.fn()
}

type tokenConnectionTestTimers struct {
	mu        sync.Mutex
	durations []time.Duration
	timers    []*tokenConnectionTestTimer
}

func (t *tokenConnectionTestTimers) AfterFunc(delay time.Duration, fn func()) connectionTimer {
	t.mu.Lock()
	defer t.mu.Unlock()
	timer := &tokenConnectionTestTimer{fn: fn}
	t.durations = append(t.durations, delay)
	t.timers = append(t.timers, timer)
	return timer
}

func (t *tokenConnectionTestTimers) Snapshot() ([]time.Duration, []*tokenConnectionTestTimer) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]time.Duration(nil), t.durations...), append([]*tokenConnectionTestTimer(nil), t.timers...)
}

type tokenConnectionTestConn struct{}

func (*tokenConnectionTestConn) Connection() net.Conn { return nil }
func (*tokenConnectionTestConn) Disconnect() error    { return nil }
func (*tokenConnectionTestConn) Send(context.Context, *protobufs.ServerToAgent) error {
	return nil
}

func newTokenConnectionTestSession(tokenID string, expiresAt *time.Time) *tokenSession {
	return &tokenSession{principal: models.OpAMPTokenPrincipal{ID: tokenID, ExpiresAt: expiresAt}}
}

func newTokenConnectionTestManager(
	clock *tokenConnectionTestClock,
	timers *tokenConnectionTestTimers,
	onExpired func([]*tokenSession),
) *tokenConnections {
	return newTokenConnections(clock.Now, timers.AfterFunc, onExpired)
}

func waitForTokenDisableState(t *testing.T, manager *tokenConnections, tokenID string) *tokenDisableState {
	t.Helper()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		manager.mu.Lock()
		state := manager.disableStates[tokenID]
		manager.mu.Unlock()
		if state != nil {
			return state
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatalf("token %s did not install a disable state", tokenID)
		}
	}
}

func waitForTokenStopState(t *testing.T, manager *tokenConnections) {
	t.Helper()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		manager.mu.Lock()
		stopped := manager.stopped
		manager.mu.Unlock()
		if stopped {
			return
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatal("connection manager did not enter the stopped state")
		}
	}
}

func waitForTokenRemoveState(t *testing.T, manager *tokenConnections, session *tokenSession) *tokenRemoveState {
	t.Helper()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		manager.mu.Lock()
		state := manager.removals[session]
		manager.mu.Unlock()
		if state != nil {
			return state
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatal("session did not register a removal state")
		}
	}
}

func completeTokenRemoveAndWait(t *testing.T, manager *tokenConnections, session *tokenSession) {
	t.Helper()
	manager.mu.Lock()
	state := manager.removals[session]
	manager.mu.Unlock()
	if state == nil {
		t.Fatal("session has no registered removal")
	}
	manager.CompleteRemove(session)
	select {
	case <-state.done:
	case <-time.After(time.Second):
		t.Fatal("session removal did not complete")
	}
}

func TestTokenConnectionsTracksMultipleSessionsAndIsolatesTokens(t *testing.T) {
	now := time.Date(2026, 7, 22, 9, 0, 0, 0, time.UTC)
	clock := &tokenConnectionTestClock{now: now}
	timers := &tokenConnectionTestTimers{}
	manager := newTokenConnectionTestManager(clock, timers, nil)

	a1 := newTokenConnectionTestSession("token-a", nil)
	a2 := newTokenConnectionTestSession("token-a", nil)
	b1 := newTokenConnectionTestSession("token-b", nil)
	for _, session := range []*tokenSession{a1, a2, b1} {
		if !manager.Track(session, &tokenConnectionTestConn{}) {
			t.Fatalf("Track(%s) = false, want true", session.principal.ID)
		}
	}

	disabled := manager.Disable("token-a")
	if len(disabled) != 2 {
		t.Fatalf("Disable(token-a) returned %d sessions, want 2", len(disabled))
	}
	if lease, ok := manager.Acquire(a1, now); ok || lease != nil {
		t.Fatal("disabled token-a session acquired a lease")
	}
	lease, ok := manager.Acquire(b1, now)
	if !ok || lease == nil {
		t.Fatal("disabling token-a prevented a token-b lease")
	}
	lease.Release()
}

func TestTokenConnectionsDisableIsIdempotentAndTombstonesFutureTracks(t *testing.T) {
	now := time.Date(2026, 7, 22, 9, 0, 0, 0, time.UTC)
	clock := &tokenConnectionTestClock{now: now}
	manager := newTokenConnectionTestManager(clock, &tokenConnectionTestTimers{}, nil)
	session := newTokenConnectionTestSession("token-a", nil)
	if !manager.Track(session, &tokenConnectionTestConn{}) {
		t.Fatal("initial Track = false, want true")
	}

	if got := len(manager.Disable("token-a")); got != 1 {
		t.Fatalf("first Disable returned %d sessions, want 1", got)
	}
	if got := len(manager.Disable("token-a")); got != 0 {
		t.Fatalf("second Disable returned %d sessions, want 0", got)
	}
	if manager.Track(newTokenConnectionTestSession("token-a", nil), &tokenConnectionTestConn{}) {
		t.Fatal("Track accepted a session after the token tombstone")
	}
}

func TestTokenConnectionsDisableWaitsForAdmittedLease(t *testing.T) {
	now := time.Date(2026, 7, 22, 9, 0, 0, 0, time.UTC)
	clock := &tokenConnectionTestClock{now: now}
	manager := newTokenConnectionTestManager(clock, &tokenConnectionTestTimers{}, nil)
	session := newTokenConnectionTestSession("token-a", nil)
	if !manager.Track(session, &tokenConnectionTestConn{}) {
		t.Fatal("Track = false, want true")
	}
	lease, ok := manager.Acquire(session, now)
	if !ok {
		t.Fatal("Acquire = false, want true")
	}

	disabled := make(chan []*tokenSession, 1)
	go func() { disabled <- manager.Disable("token-a") }()
	waitForTokenDisableState(t, manager, session.principal.ID)

	select {
	case <-disabled:
		t.Fatal("Disable returned before the admitted lease was released")
	case <-time.After(30 * time.Millisecond):
	}
	if second, acquired := manager.Acquire(session, now); acquired || second != nil {
		t.Fatal("a new lease started after Disable installed its tombstone")
	}

	lease.Release()
	select {
	case sessions := <-disabled:
		if len(sessions) != 1 || sessions[0] != session {
			t.Fatalf("Disable returned %#v, want the tracked session", sessions)
		}
	case <-time.After(time.Second):
		t.Fatal("Disable did not return after the lease was released")
	}

	// Release must be idempotent; a second call must not panic or corrupt the gate.
	lease.Release()
}

func TestTokenConnectionsConcurrentDisableJoinsWinner(t *testing.T) {
	now := time.Date(2026, 7, 22, 9, 0, 0, 0, time.UTC)
	clock := &tokenConnectionTestClock{now: now}
	manager := newTokenConnectionTestManager(clock, &tokenConnectionTestTimers{}, nil)
	session := newTokenConnectionTestSession("token-a", nil)
	if !manager.Track(session, &tokenConnectionTestConn{}) {
		t.Fatal("Track = false, want true")
	}
	lease, ok := manager.Acquire(session, now)
	if !ok {
		t.Fatal("Acquire = false, want true")
	}

	firstDone := make(chan []*tokenSession, 1)
	go func() { firstDone <- manager.Disable("token-a") }()
	waitForTokenDisableState(t, manager, session.principal.ID)

	secondDone := make(chan []*tokenSession, 1)
	go func() { secondDone <- manager.Disable("token-a") }()
	select {
	case <-secondDone:
		t.Fatal("concurrent Disable loser returned before the winner drained its lease")
	case <-time.After(30 * time.Millisecond):
	}

	lease.Release()
	first := <-firstDone
	second := <-secondDone
	if len(first) != 1 || first[0] != session {
		t.Fatalf("winning Disable returned %#v, want the tracked session", first)
	}
	if len(second) != 0 {
		t.Fatalf("joining Disable returned %d sessions, want 0", len(second))
	}
}

func TestTokenConnectionsDisableLoserWaitsForCleanupCompletion(t *testing.T) {
	now := time.Date(2026, 7, 22, 9, 0, 0, 0, time.UTC)
	clock := &tokenConnectionTestClock{now: now}
	cleanupStarted := make(chan struct{})
	cleanupRelease := make(chan struct{})
	manager := newTokenConnectionTestManager(clock, &tokenConnectionTestTimers{}, func(_ []*tokenSession) {
		close(cleanupStarted)
		<-cleanupRelease
	})
	session := newTokenConnectionTestSession("token-a", nil)
	if !manager.Track(session, &tokenConnectionTestConn{}) {
		t.Fatal("Track = false, want true")
	}

	firstDone := make(chan []*tokenSession, 1)
	go func() { firstDone <- manager.Disable("token-a") }()
	select {
	case <-cleanupStarted:
	case <-time.After(time.Second):
		t.Fatal("winning Disable did not enter cleanup")
	}
	secondDone := make(chan []*tokenSession, 1)
	go func() { secondDone <- manager.Disable("token-a") }()
	select {
	case <-firstDone:
		close(cleanupRelease)
		t.Fatal("Disable winner returned before cleanup completed")
	case <-secondDone:
		close(cleanupRelease)
		t.Fatal("Disable loser returned before cleanup completed")
	case <-time.After(30 * time.Millisecond):
	}
	close(cleanupRelease)
	if sessions := <-firstDone; len(sessions) != 1 || sessions[0] != session {
		t.Fatalf("winning Disable returned %#v, want the tracked session", sessions)
	}
	if sessions := <-secondDone; len(sessions) != 0 {
		t.Fatalf("joining Disable returned %d sessions, want 0", len(sessions))
	}
}

func TestTokenConnectionsTimerDisableLoserJoinsActiveLeaseDrain(t *testing.T) {
	now := time.Date(2026, 7, 22, 9, 0, 0, 0, time.UTC)
	expiresAt := now.Add(time.Minute)
	clock := &tokenConnectionTestClock{now: now}
	timers := &tokenConnectionTestTimers{}
	expired := make(chan []*tokenSession, 1)
	manager := newTokenConnectionTestManager(clock, timers, func(sessions []*tokenSession) {
		expired <- sessions
	})
	session := newTokenConnectionTestSession("token-a", &expiresAt)
	if !manager.Track(session, &tokenConnectionTestConn{}) {
		t.Fatal("Track = false, want true")
	}
	lease, ok := manager.Acquire(session, now)
	if !ok {
		t.Fatal("Acquire = false, want true")
	}
	_, scheduled := timers.Snapshot()
	clock.Set(expiresAt)
	timerDone := make(chan struct{})
	go func() {
		scheduled[0].Fire()
		close(timerDone)
	}()
	waitForTokenDisableState(t, manager, session.principal.ID)

	manualDone := make(chan []*tokenSession, 1)
	go func() { manualDone <- manager.Disable("token-a") }()
	select {
	case <-manualDone:
		t.Fatal("manual Disable loser returned before timer winner drained the lease")
	case <-time.After(30 * time.Millisecond):
	}

	lease.Release()
	select {
	case sessions := <-expired:
		if len(sessions) != 1 || sessions[0] != session {
			t.Fatalf("expiry cleanup returned %#v, want the tracked session", sessions)
		}
	case <-time.After(time.Second):
		t.Fatal("timer winner did not clean the expired session")
	}
	select {
	case sessions := <-manualDone:
		if len(sessions) != 0 {
			t.Fatalf("manual Disable loser returned %d sessions, want 0", len(sessions))
		}
	case <-time.After(time.Second):
		t.Fatal("manual Disable loser did not join timer completion")
	}
	select {
	case <-timerDone:
	case <-time.After(time.Second):
		t.Fatal("timer callback did not finish")
	}
}

func TestTokenConnectionsRejectsExactExpiryWithoutTimerCallback(t *testing.T) {
	now := time.Date(2026, 7, 22, 9, 0, 0, 0, time.UTC)
	expiresAt := now.Add(time.Minute)
	clock := &tokenConnectionTestClock{now: now}
	timers := &tokenConnectionTestTimers{}
	expired := make(chan []*tokenSession, 1)
	manager := newTokenConnectionTestManager(clock, timers, func(sessions []*tokenSession) {
		expired <- sessions
	})
	session := newTokenConnectionTestSession("token-a", &expiresAt)
	if !manager.Track(session, &tokenConnectionTestConn{}) {
		t.Fatal("Track = false, want true")
	}

	clock.Set(expiresAt)
	if lease, ok := manager.Acquire(session, expiresAt); ok || lease != nil {
		t.Fatal("Acquire accepted now == expires_at")
	}
	select {
	case sessions := <-expired:
		if len(sessions) != 1 || sessions[0] != session {
			t.Fatalf("expiry callback returned %#v, want the tracked session", sessions)
		}
	case <-time.After(time.Second):
		t.Fatal("synchronous expiry did not invoke the cleanup callback")
	}
}

func TestTokenConnectionsTrackRejectsExactExpiry(t *testing.T) {
	expiresAt := time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)
	clock := &tokenConnectionTestClock{now: expiresAt}
	manager := newTokenConnectionTestManager(clock, &tokenConnectionTestTimers{}, nil)

	if manager.Track(newTokenConnectionTestSession("token-a", &expiresAt), &tokenConnectionTestConn{}) {
		t.Fatal("Track accepted now == expires_at")
	}
}

func TestTokenConnectionsEarlyTimerCallbackRearmsUntilDeadline(t *testing.T) {
	now := time.Date(2026, 7, 22, 9, 0, 0, 0, time.UTC)
	expiresAt := now.Add(time.Minute)
	clock := &tokenConnectionTestClock{now: now}
	timers := &tokenConnectionTestTimers{}
	expired := make(chan []*tokenSession, 1)
	manager := newTokenConnectionTestManager(clock, timers, func(sessions []*tokenSession) {
		expired <- sessions
	})
	session := newTokenConnectionTestSession("token-a", &expiresAt)
	if !manager.Track(session, &tokenConnectionTestConn{}) {
		t.Fatal("Track = false, want true")
	}
	_, scheduled := timers.Snapshot()

	clock.Set(now.Add(20 * time.Second))
	scheduled[0].Fire()
	select {
	case <-expired:
		t.Fatal("early timer callback expired the token")
	default:
	}
	durations, rearmed := timers.Snapshot()
	if len(rearmed) != 2 {
		t.Fatalf("scheduled timers = %d, want an early rearm", len(rearmed))
	}
	if durations[1] != 40*time.Second {
		t.Fatalf("rearm delay = %s, want 40s", durations[1])
	}
	lease, ok := manager.Acquire(session, clock.Now())
	if !ok {
		t.Fatal("session inactive after early timer callback")
	}
	lease.Release()
}

func TestTokenConnectionsRemoveDuringLeaseKeepsDisableBarrierVisible(t *testing.T) {
	now := time.Date(2026, 7, 22, 9, 0, 0, 0, time.UTC)
	clock := &tokenConnectionTestClock{now: now}
	manager := newTokenConnectionTestManager(clock, &tokenConnectionTestTimers{}, nil)
	session := newTokenConnectionTestSession("token-a", nil)
	if !manager.Track(session, &tokenConnectionTestConn{}) {
		t.Fatal("Track = false, want true")
	}
	lease, ok := manager.Acquire(session, now)
	if !ok {
		t.Fatal("Acquire = false, want true")
	}

	removeDone := make(chan struct{})
	go func() {
		manager.Remove(session)
		manager.CompleteRemove(session)
		close(removeDone)
	}()
	select {
	case <-removeDone:
	case <-time.After(time.Second):
		t.Fatal("Remove blocked behind an active lease")
	}

	disableDone := make(chan []*tokenSession, 1)
	go func() { disableDone <- manager.Disable("token-a") }()
	waitForTokenDisableState(t, manager, session.principal.ID)
	select {
	case <-disableDone:
		t.Fatal("Disable lost visibility of a draining leased session")
	case <-time.After(30 * time.Millisecond):
	}
	lease.Release()
	select {
	case sessions := <-disableDone:
		if len(sessions) != 1 || sessions[0] != session {
			t.Fatalf("Disable returned %#v, want the draining session", sessions)
		}
	case <-time.After(time.Second):
		t.Fatal("Disable did not complete after the draining lease")
	}
}

func TestTokenConnectionsStopWaitsForRemoveCleanupWithoutLease(t *testing.T) {
	now := time.Date(2026, 7, 22, 9, 0, 0, 0, time.UTC)
	clock := &tokenConnectionTestClock{now: now}
	manager := newTokenConnectionTestManager(clock, &tokenConnectionTestTimers{}, nil)
	session := newTokenConnectionTestSession("token-a", nil)
	if !manager.Track(session, &tokenConnectionTestConn{}) {
		t.Fatal("Track = false, want true")
	}
	manager.Remove(session)

	stopDone := make(chan []*tokenSession, 1)
	go func() { stopDone <- manager.Stop() }()
	waitForTokenStopState(t, manager)
	select {
	case <-stopDone:
		t.Fatal("Stop returned before removal cleanup completed")
	case <-time.After(30 * time.Millisecond):
	}
	manager.CompleteRemove(session)
	select {
	case sessions := <-stopDone:
		if len(sessions) != 1 || sessions[0] != session {
			t.Fatalf("Stop returned %#v, want the removed session", sessions)
		}
	case <-time.After(time.Second):
		t.Fatal("Stop did not finish after removal cleanup")
	}
}

func TestTokenConnectionsDisableJoinsPreexistingRemoveCleanup(t *testing.T) {
	now := time.Date(2026, 7, 22, 9, 0, 0, 0, time.UTC)
	clock := &tokenConnectionTestClock{now: now}
	manager := newTokenConnectionTestManager(clock, &tokenConnectionTestTimers{}, nil)
	session := newTokenConnectionTestSession("token-a", nil)
	if !manager.Track(session, &tokenConnectionTestConn{}) {
		t.Fatal("Track = false, want true")
	}
	manager.Remove(session)

	disableDone := make(chan []*tokenSession, 1)
	go func() { disableDone <- manager.Disable("token-a") }()
	waitForTokenDisableState(t, manager, session.principal.ID)
	select {
	case <-disableDone:
		t.Fatal("Disable returned before preexisting removal cleanup completed")
	case <-time.After(30 * time.Millisecond):
	}
	manager.CompleteRemove(session)
	select {
	case sessions := <-disableDone:
		if len(sessions) != 1 || sessions[0] != session {
			t.Fatalf("Disable returned %#v, want the removed session", sessions)
		}
	case <-time.After(time.Second):
		t.Fatal("Disable did not finish after preexisting removal cleanup")
	}
}

func TestTokenConnectionsRemoveAndCompleteAreIdempotent(t *testing.T) {
	now := time.Date(2026, 7, 22, 9, 0, 0, 0, time.UTC)
	clock := &tokenConnectionTestClock{now: now}
	manager := newTokenConnectionTestManager(clock, &tokenConnectionTestTimers{}, nil)
	session := newTokenConnectionTestSession("token-a", nil)
	if !manager.Track(session, &tokenConnectionTestConn{}) {
		t.Fatal("Track = false, want true")
	}
	manager.Remove(session)
	manager.Remove(session)
	manager.CompleteRemove(session)
	manager.CompleteRemove(session)

	done := make(chan []*tokenSession, 1)
	go func() { done <- manager.Stop() }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("idempotent removal left Stop blocked")
	}
}

func TestTokenConnectionsTimerDisableRaceCleansSessionOnce(t *testing.T) {
	now := time.Date(2026, 7, 22, 9, 0, 0, 0, time.UTC)
	expiresAt := now.Add(time.Minute)
	clock := &tokenConnectionTestClock{now: now}
	timers := &tokenConnectionTestTimers{}
	expired := make(chan []*tokenSession, 1)
	manager := newTokenConnectionTestManager(clock, timers, func(sessions []*tokenSession) {
		expired <- sessions
	})
	session := newTokenConnectionTestSession("token-a", &expiresAt)
	if !manager.Track(session, &tokenConnectionTestConn{}) {
		t.Fatal("Track = false, want true")
	}
	_, scheduled := timers.Snapshot()
	clock.Set(expiresAt)

	start := make(chan struct{})
	manualResult := make(chan []*tokenSession, 1)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		scheduled[0].Fire()
	}()
	go func() {
		defer wg.Done()
		<-start
		manualResult <- manager.Disable("token-a")
	}()
	close(start)
	wg.Wait()
	manual := <-manualResult
	select {
	case cleaned := <-expired:
		if len(cleaned) != 1 || cleaned[0] != session {
			t.Fatalf("cleanup callback returned %#v, want the tracked session", cleaned)
		}
	case <-time.After(time.Second):
		t.Fatal("timer/Disable winner did not run cleanup")
	}
	select {
	case duplicate := <-expired:
		t.Fatalf("timer/Disable race ran duplicate cleanup for %#v", duplicate)
	default:
	}
	if len(manual) > 1 || len(manual) == 1 && manual[0] != session {
		t.Fatalf("manual Disable returned %#v, want zero sessions as loser or the tracked session as winner", manual)
	}
	if lease, ok := manager.Acquire(session, expiresAt); ok || lease != nil {
		t.Fatal("timer/Disable race left the session active")
	}
}

func TestTokenConnectionsIgnoresStaleTimerGeneration(t *testing.T) {
	now := time.Date(2026, 7, 22, 9, 0, 0, 0, time.UTC)
	firstExpiry := now.Add(time.Minute)
	secondExpiry := now.Add(2 * time.Minute)
	clock := &tokenConnectionTestClock{now: now}
	timers := &tokenConnectionTestTimers{}
	expired := make(chan []*tokenSession, 2)
	manager := newTokenConnectionTestManager(clock, timers, func(sessions []*tokenSession) {
		expired <- sessions
	})

	first := newTokenConnectionTestSession("token-a", &firstExpiry)
	if !manager.Track(first, &tokenConnectionTestConn{}) {
		t.Fatal("first Track = false, want true")
	}
	manager.Remove(first)
	completeTokenRemoveAndWait(t, manager, first)
	second := newTokenConnectionTestSession("token-a", &secondExpiry)
	if !manager.Track(second, &tokenConnectionTestConn{}) {
		t.Fatal("second Track = false, want true")
	}
	_, scheduled := timers.Snapshot()
	if len(scheduled) != 2 {
		t.Fatalf("scheduled timers = %d, want 2 generations", len(scheduled))
	}

	clock.Set(firstExpiry)
	scheduled[0].Fire()
	select {
	case <-expired:
		t.Fatal("stale timer generation disabled the replacement session")
	default:
	}
	lease, ok := manager.Acquire(second, firstExpiry)
	if !ok {
		t.Fatal("replacement session became inactive after stale callback")
	}
	lease.Release()

	clock.Set(secondExpiry)
	scheduled[1].Fire()
	select {
	case sessions := <-expired:
		if len(sessions) != 1 || sessions[0] != second {
			t.Fatalf("current timer returned %#v, want replacement session", sessions)
		}
	case <-time.After(time.Second):
		t.Fatal("current timer generation did not expire the token")
	}
}

func TestTokenConnectionsUsesOneTimerForMultipleSessionsOfToken(t *testing.T) {
	now := time.Date(2026, 7, 22, 9, 0, 0, 0, time.UTC)
	expiresAt := now.Add(time.Minute)
	clock := &tokenConnectionTestClock{now: now}
	timers := &tokenConnectionTestTimers{}
	manager := newTokenConnectionTestManager(clock, timers, nil)

	for range 2 {
		if !manager.Track(newTokenConnectionTestSession("token-a", &expiresAt), &tokenConnectionTestConn{}) {
			t.Fatal("Track = false, want true")
		}
	}
	durations, scheduled := timers.Snapshot()
	if len(scheduled) != 1 {
		t.Fatalf("scheduled timers = %d, want 1", len(scheduled))
	}
	if durations[0] != time.Minute {
		t.Fatalf("timer delay = %s, want 1m", durations[0])
	}
}

func TestTokenConnectionsRemoveBeforeFirstMessage(t *testing.T) {
	now := time.Date(2026, 7, 22, 9, 0, 0, 0, time.UTC)
	clock := &tokenConnectionTestClock{now: now}
	manager := newTokenConnectionTestManager(clock, &tokenConnectionTestTimers{}, nil)
	session := newTokenConnectionTestSession("token-a", nil)
	if !manager.Track(session, &tokenConnectionTestConn{}) {
		t.Fatal("Track = false, want true")
	}

	manager.Remove(session)
	if lease, ok := manager.Acquire(session, now); ok || lease != nil {
		t.Fatal("removed pre-message session acquired a lease")
	}
	// Remove is intentionally idempotent for late close callbacks.
	manager.Remove(session)
	manager.CompleteRemove(session)
}

func TestTokenConnectionsTrackDisableRaceNeverLeavesActiveSession(t *testing.T) {
	now := time.Date(2026, 7, 22, 9, 0, 0, 0, time.UTC)
	for i := 0; i < 100; i++ {
		clock := &tokenConnectionTestClock{now: now}
		manager := newTokenConnectionTestManager(clock, &tokenConnectionTestTimers{}, nil)
		session := newTokenConnectionTestSession("token-a", nil)
		start := make(chan struct{})
		var tracked bool
		var disabled []*tokenSession
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			tracked = manager.Track(session, &tokenConnectionTestConn{})
		}()
		go func() {
			defer wg.Done()
			<-start
			disabled = manager.Disable("token-a")
		}()
		close(start)
		wg.Wait()

		if tracked && len(disabled) != 1 {
			t.Fatalf("iteration %d: Track won but Disable returned %d sessions", i, len(disabled))
		}
		if lease, ok := manager.Acquire(session, now); ok || lease != nil {
			t.Fatalf("iteration %d: raced session remained active", i)
		}
	}
}

func TestTokenConnectionsStopReturnsSessionsAndRejectsFutureWork(t *testing.T) {
	now := time.Date(2026, 7, 22, 9, 0, 0, 0, time.UTC)
	clock := &tokenConnectionTestClock{now: now}
	manager := newTokenConnectionTestManager(clock, &tokenConnectionTestTimers{}, nil)
	sessions := []*tokenSession{
		newTokenConnectionTestSession("token-a", nil),
		newTokenConnectionTestSession("token-b", nil),
	}
	for _, session := range sessions {
		if !manager.Track(session, &tokenConnectionTestConn{}) {
			t.Fatal("Track = false, want true")
		}
	}

	stopped := manager.Stop()
	if len(stopped) != len(sessions) {
		t.Fatalf("Stop returned %d sessions, want %d", len(stopped), len(sessions))
	}
	if got := len(manager.Stop()); got != 0 {
		t.Fatalf("second Stop returned %d sessions, want 0", got)
	}
	for _, session := range sessions {
		if lease, ok := manager.Acquire(session, now); ok || lease != nil {
			t.Fatal("stopped session acquired a lease")
		}
	}
	if manager.Track(newTokenConnectionTestSession("token-c", nil), &tokenConnectionTestConn{}) {
		t.Fatal("Track accepted a session after Stop")
	}
}

func TestTokenConnectionsStopWaitsForActiveLease(t *testing.T) {
	now := time.Date(2026, 7, 22, 9, 0, 0, 0, time.UTC)
	clock := &tokenConnectionTestClock{now: now}
	manager := newTokenConnectionTestManager(clock, &tokenConnectionTestTimers{}, nil)
	session := newTokenConnectionTestSession("token-a", nil)
	if !manager.Track(session, &tokenConnectionTestConn{}) {
		t.Fatal("Track = false, want true")
	}
	lease, ok := manager.Acquire(session, now)
	if !ok {
		t.Fatal("Acquire = false, want true")
	}

	stopDone := make(chan []*tokenSession, 1)
	go func() { stopDone <- manager.Stop() }()
	waitForTokenStopState(t, manager)
	select {
	case <-stopDone:
		t.Fatal("Stop returned before the active lease was released")
	case <-time.After(30 * time.Millisecond):
	}
	lease.Release()
	select {
	case sessions := <-stopDone:
		if len(sessions) != 1 || sessions[0] != session {
			t.Fatalf("Stop returned %#v, want the active session", sessions)
		}
	case <-time.After(time.Second):
		t.Fatal("Stop did not finish after the active lease")
	}
}

func TestTokenConnectionsConcurrentStopJoinsWinner(t *testing.T) {
	now := time.Date(2026, 7, 22, 9, 0, 0, 0, time.UTC)
	clock := &tokenConnectionTestClock{now: now}
	manager := newTokenConnectionTestManager(clock, &tokenConnectionTestTimers{}, nil)
	session := newTokenConnectionTestSession("token-a", nil)
	if !manager.Track(session, &tokenConnectionTestConn{}) {
		t.Fatal("Track = false, want true")
	}
	lease, ok := manager.Acquire(session, now)
	if !ok {
		t.Fatal("Acquire = false, want true")
	}

	firstDone := make(chan []*tokenSession, 1)
	go func() { firstDone <- manager.Stop() }()
	waitForTokenStopState(t, manager)

	secondStarted := make(chan struct{})
	secondDone := make(chan []*tokenSession, 1)
	go func() {
		close(secondStarted)
		secondDone <- manager.Stop()
	}()
	<-secondStarted
	select {
	case <-secondDone:
		lease.Release()
		t.Fatal("concurrent Stop follower returned before the winner completed")
	case <-time.After(30 * time.Millisecond):
	}

	lease.Release()
	select {
	case sessions := <-firstDone:
		if len(sessions) != 1 || sessions[0] != session {
			t.Fatalf("winning Stop returned %#v, want the active session", sessions)
		}
	case <-time.After(time.Second):
		t.Fatal("winning Stop did not finish after the active lease")
	}
	select {
	case sessions := <-secondDone:
		if len(sessions) != 0 {
			t.Fatalf("joining Stop returned %d sessions, want 0", len(sessions))
		}
	case <-time.After(time.Second):
		t.Fatal("concurrent Stop follower did not join the winner")
	}
}

func TestTokenConnectionsStopIncludesRemovedDrainingSession(t *testing.T) {
	now := time.Date(2026, 7, 22, 9, 0, 0, 0, time.UTC)
	clock := &tokenConnectionTestClock{now: now}
	manager := newTokenConnectionTestManager(clock, &tokenConnectionTestTimers{}, nil)
	session := newTokenConnectionTestSession("token-a", nil)
	if !manager.Track(session, &tokenConnectionTestConn{}) {
		t.Fatal("Track = false, want true")
	}
	lease, ok := manager.Acquire(session, now)
	if !ok {
		t.Fatal("Acquire = false, want true")
	}
	manager.Remove(session)
	manager.CompleteRemove(session)

	stopDone := make(chan []*tokenSession, 1)
	go func() { stopDone <- manager.Stop() }()
	waitForTokenStopState(t, manager)
	select {
	case <-stopDone:
		t.Fatal("Stop returned while a removed session was still draining")
	case <-time.After(30 * time.Millisecond):
	}
	lease.Release()
	select {
	case sessions := <-stopDone:
		if len(sessions) != 1 || sessions[0] != session {
			t.Fatalf("Stop returned %#v, want the removed draining session", sessions)
		}
	case <-time.After(time.Second):
		t.Fatal("Stop did not finish after the removed draining session")
	}
}

func TestTokenConnectionsStopWaitsForInFlightExpiryCallback(t *testing.T) {
	now := time.Date(2026, 7, 22, 9, 0, 0, 0, time.UTC)
	expiresAt := now.Add(time.Minute)
	clock := &tokenConnectionTestClock{now: now}
	timers := &tokenConnectionTestTimers{}
	callbackStarted := make(chan struct{})
	callbackRelease := make(chan struct{})
	manager := newTokenConnectionTestManager(clock, timers, func(_ []*tokenSession) {
		close(callbackStarted)
		<-callbackRelease
	})
	session := newTokenConnectionTestSession("token-a", &expiresAt)
	if !manager.Track(session, &tokenConnectionTestConn{}) {
		t.Fatal("Track = false, want true")
	}
	_, scheduled := timers.Snapshot()
	clock.Set(expiresAt)
	timerDone := make(chan struct{})
	go func() {
		scheduled[0].Fire()
		close(timerDone)
	}()
	select {
	case <-callbackStarted:
	case <-time.After(time.Second):
		t.Fatal("expiry callback did not start")
	}

	stopDone := make(chan []*tokenSession, 1)
	go func() { stopDone <- manager.Stop() }()
	waitForTokenStopState(t, manager)
	select {
	case <-stopDone:
		t.Fatal("Stop returned while an expiry cleanup callback was in flight")
	case <-time.After(30 * time.Millisecond):
	}
	close(callbackRelease)
	select {
	case <-timerDone:
	case <-time.After(time.Second):
		t.Fatal("expiry callback did not finish")
	}
	select {
	case <-stopDone:
	case <-time.After(time.Second):
		t.Fatal("Stop did not finish after the expiry callback")
	}
}

func TestTokenConnectionsStopWaitsForExpiryCallbackBlockedInClock(t *testing.T) {
	now := time.Date(2026, 7, 22, 9, 0, 0, 0, time.UTC)
	expiresAt := now.Add(time.Minute)
	clock := &tokenConnectionTestClock{now: now}
	timers := &tokenConnectionTestTimers{}
	manager := newTokenConnectionTestManager(clock, timers, nil)
	session := newTokenConnectionTestSession("token-a", &expiresAt)
	if !manager.Track(session, &tokenConnectionTestConn{}) {
		t.Fatal("Track = false, want true")
	}
	_, scheduled := timers.Snapshot()
	clock.Set(expiresAt)
	nowStarted := make(chan struct{})
	nowRelease := make(chan struct{})
	var nowOnce sync.Once
	manager.now = func() time.Time {
		nowOnce.Do(func() { close(nowStarted) })
		<-nowRelease
		return clock.Now()
	}

	timerDone := make(chan struct{})
	go func() {
		scheduled[0].Fire()
		close(timerDone)
	}()
	select {
	case <-nowStarted:
	case <-time.After(time.Second):
		t.Fatal("expiry callback did not enter the injected clock")
	}

	stopDone := make(chan []*tokenSession, 1)
	go func() { stopDone <- manager.Stop() }()
	waitForTokenStopState(t, manager)
	select {
	case <-stopDone:
		close(nowRelease)
		t.Fatal("Stop returned while an admitted expiry callback was blocked in now")
	case <-time.After(30 * time.Millisecond):
	}

	close(nowRelease)
	select {
	case <-timerDone:
	case <-time.After(time.Second):
		t.Fatal("expiry callback did not finish after releasing the clock")
	}
	select {
	case <-stopDone:
	case <-time.After(time.Second):
		t.Fatal("Stop did not finish after the expiry callback")
	}
}

func TestTokenConnectionsTimerStartRaceWithStopCleansSessionExactlyOnce(t *testing.T) {
	now := time.Date(2026, 7, 22, 9, 0, 0, 0, time.UTC)
	for iteration := range 100 {
		expiresAt := now.Add(time.Minute)
		clock := &tokenConnectionTestClock{now: now}
		timers := &tokenConnectionTestTimers{}
		expired := make(chan []*tokenSession, 1)
		manager := newTokenConnectionTestManager(clock, timers, func(sessions []*tokenSession) {
			expired <- sessions
		})
		session := newTokenConnectionTestSession("token-a", &expiresAt)
		if !manager.Track(session, &tokenConnectionTestConn{}) {
			t.Fatalf("iteration %d: Track = false, want true", iteration)
		}
		_, scheduled := timers.Snapshot()
		clock.Set(expiresAt)
		start := make(chan struct{})
		stopResult := make(chan []*tokenSession, 1)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			scheduled[0].Fire()
		}()
		go func() {
			defer wg.Done()
			<-start
			stopResult <- manager.Stop()
		}()
		close(start)
		wg.Wait()
		stopped := <-stopResult
		if len(stopped) > 1 || len(stopped) == 1 && stopped[0] != session {
			t.Fatalf("iteration %d: Stop returned %#v, want zero sessions or the tracked session", iteration, stopped)
		}
		select {
		case cleaned := <-expired:
			if len(cleaned) != 1 || cleaned[0] != session {
				t.Fatalf("iteration %d: cleanup callback returned %#v, want the tracked session", iteration, cleaned)
			}
		case <-time.After(time.Second):
			t.Fatalf("iteration %d: timer/Stop winner did not clean the session", iteration)
		}
		select {
		case duplicate := <-expired:
			t.Fatalf("iteration %d: timer/Stop race ran duplicate cleanup for %#v", iteration, duplicate)
		default:
		}
		if lease, ok := manager.Acquire(session, expiresAt); ok || lease != nil {
			t.Fatalf("iteration %d: timer/Stop race left session active", iteration)
		}
	}
}

var _ types.Connection = (*tokenConnectionTestConn)(nil)
