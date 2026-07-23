package opamp

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/open-telemetry/opamp-go/server/types"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

type connectionTimer interface {
	Stop() bool
}

type connectionAfterFunc func(time.Duration, func()) connectionTimer

type tokenSession struct {
	principal models.OpAMPTokenPrincipal
	conn      types.Connection

	gate             sync.RWMutex
	sendGate         sync.Mutex
	leases           atomic.Int64
	messageGate      sync.Mutex
	httpMu           sync.Mutex
	httpLease        *tokenLease
	httpOwnsSendGate bool
	disconnectOnce   sync.Once
	releaseOnce      sync.Once
	uid              string
	admitted         bool
	terminal         bool
}

func (s *tokenSession) holdHTTPResponse(lease *tokenLease) bool {
	s.httpMu.Lock()
	defer s.httpMu.Unlock()
	if lease == nil || s.httpLease != nil || s.httpOwnsSendGate {
		return false
	}
	s.httpLease = lease
	s.httpOwnsSendGate = true
	return true
}

func (s *tokenSession) finishHTTPResponse() {
	s.httpMu.Lock()
	lease := s.httpLease
	ownsSendGate := s.httpOwnsSendGate
	s.httpLease = nil
	s.httpOwnsSendGate = false
	s.httpMu.Unlock()
	if lease == nil && !ownsSendGate {
		return
	}
	s.clearWriteDeadline()
	if lease != nil {
		lease.Release()
	}
	if ownsSendGate {
		s.sendGate.Unlock()
	}
}

func (s *tokenSession) clearWriteDeadline() {
	if conn := s.conn.Connection(); conn != nil {
		if err := conn.SetWriteDeadline(time.Time{}); err != nil {
			_ = conn.Close()
		}
	}
}

type tokenLease struct {
	manager *tokenConnections
	session *tokenSession
	once    sync.Once
}

func (l *tokenLease) Release() {
	if l == nil || l.session == nil {
		return
	}
	l.once.Do(func() {
		l.session.gate.RUnlock()
		l.session.leases.Add(-1)
	})
}

func (l *tokenLease) Fork() (*tokenLease, bool) {
	if l == nil || l.manager == nil || l.session == nil {
		return nil, false
	}
	m := l.manager
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopped || m.disableStates[l.session.principal.ID] != nil {
		return nil, false
	}
	if _, tracked := m.sessions[l.session]; !tracked {
		return nil, false
	}
	l.session.gate.RLock()
	l.session.leases.Add(1)
	return &tokenLease{manager: m, session: l.session}, true
}

type tokenExpiryTimer struct {
	timer      connectionTimer
	expiresAt  time.Time
	generation uint64
}

type tokenDisableState struct {
	done          chan struct{}
	sessions      []*tokenSession
	removals      []*tokenRemoveState
	ownedRemovals []*tokenRemoveState
}

type tokenRemoveState struct {
	session     *tokenSession
	drained     chan struct{}
	cleanupDone chan struct{}
	done        chan struct{}
	complete    sync.Once
}

type tokenStopState struct {
	done          chan struct{}
	sessions      []*tokenSession
	active        []*tokenSession
	ownedRemovals []*tokenRemoveState
	removals      []*tokenRemoveState
	disableDone   []<-chan struct{}
}

type tokenConnections struct {
	mu         sync.Mutex
	now        func() time.Time
	afterFunc  connectionAfterFunc
	onDisabled func([]*tokenSession)

	sessions      map[*tokenSession]struct{}
	sessionsByID  map[string]map[*tokenSession]struct{}
	removals      map[*tokenSession]*tokenRemoveState
	removalsByID  map[string]map[*tokenSession]*tokenRemoveState
	disableStates map[string]*tokenDisableState
	expiryTimers  map[string]tokenExpiryTimer
	nextTimerID   uint64
	stopped       bool
	stopState     *tokenStopState
	expiryWG      sync.WaitGroup
	removeWG      sync.WaitGroup
}

func newTokenConnections(
	now func() time.Time,
	afterFunc connectionAfterFunc,
	onDisabled func([]*tokenSession),
) *tokenConnections {
	if now == nil {
		now = time.Now
	}
	if afterFunc == nil {
		afterFunc = func(delay time.Duration, fn func()) connectionTimer {
			return time.AfterFunc(delay, fn)
		}
	}
	return &tokenConnections{
		now:           now,
		afterFunc:     afterFunc,
		onDisabled:    onDisabled,
		sessions:      make(map[*tokenSession]struct{}),
		sessionsByID:  make(map[string]map[*tokenSession]struct{}),
		removals:      make(map[*tokenSession]*tokenRemoveState),
		removalsByID:  make(map[string]map[*tokenSession]*tokenRemoveState),
		disableStates: make(map[string]*tokenDisableState),
		expiryTimers:  make(map[string]tokenExpiryTimer),
	}
}

func (m *tokenConnections) Track(session *tokenSession, conn types.Connection) bool {
	if session == nil || conn == nil || session.principal.ID == "" {
		return false
	}

	now := m.now()
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return false
	}
	if _, disabled := m.disableStates[session.principal.ID]; disabled {
		m.mu.Unlock()
		return false
	}
	if session.principal.ExpiresAt != nil && !session.principal.ExpiresAt.After(now) {
		state, _ := m.beginDisableLocked(session.principal.ID)
		m.mu.Unlock()
		m.completeDisable(state)
		return false
	}
	if _, tracked := m.sessions[session]; tracked {
		m.mu.Unlock()
		return false
	}

	session.conn = conn
	m.sessions[session] = struct{}{}
	byID := m.sessionsByID[session.principal.ID]
	if byID == nil {
		byID = make(map[*tokenSession]struct{})
		m.sessionsByID[session.principal.ID] = byID
	}
	byID[session] = struct{}{}
	m.scheduleExpiryLocked(session.principal.ID, now)
	m.mu.Unlock()
	return true
}

func (m *tokenConnections) Acquire(session *tokenSession, now time.Time) (*tokenLease, bool) {
	if session == nil {
		return nil, false
	}

	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return nil, false
	}
	if state := m.disableStates[session.principal.ID]; state != nil {
		m.mu.Unlock()
		return nil, false
	}
	if _, tracked := m.sessions[session]; !tracked {
		m.mu.Unlock()
		return nil, false
	}
	if session.principal.ExpiresAt != nil && !session.principal.ExpiresAt.After(now) {
		state, winner := m.beginDisableLocked(session.principal.ID)
		if winner {
			m.expiryWG.Add(1)
		}
		m.mu.Unlock()
		if winner {
			go func() {
				defer m.expiryWG.Done()
				m.completeDisable(state)
			}()
		}
		return nil, false
	}

	// The global mutex fixes the admission order relative to Disable. Once this
	// read gate is held, a disabling caller must wait for the operation to end.
	session.gate.RLock()
	session.leases.Add(1)
	m.mu.Unlock()
	return &tokenLease{manager: m, session: session}, true
}

func (m *tokenConnections) Remove(session *tokenSession) {
	if session == nil {
		return
	}
	m.mu.Lock()
	if m.removals[session] != nil {
		m.mu.Unlock()
		return
	}
	if m.stopped {
		m.mu.Unlock()
		return
	}
	if _, tracked := m.sessions[session]; !tracked {
		m.mu.Unlock()
		return
	}
	m.detachSessionLocked(session)
	m.registerRemoveLocked(session)
	m.scheduleExpiryLocked(session.principal.ID, m.now())
	m.mu.Unlock()
}

func (m *tokenConnections) CompleteRemove(session *tokenSession) {
	if session == nil {
		return
	}
	m.mu.Lock()
	state := m.removals[session]
	m.mu.Unlock()
	completeTokenRemove(state)
}

func (m *tokenConnections) waitRemoveDrain(session *tokenSession) {
	if session == nil {
		return
	}
	m.mu.Lock()
	state := m.removals[session]
	m.mu.Unlock()
	if state != nil {
		<-state.drained
	}
}

func (m *tokenConnections) Disable(tokenID string) []*tokenSession {
	m.mu.Lock()
	if m.stopped {
		stopState := m.stopState
		m.mu.Unlock()
		<-stopState.done
		return nil
	}
	state, winner := m.beginDisableLocked(tokenID)
	m.mu.Unlock()
	if !winner {
		<-state.done
		return nil
	}

	return m.completeDisable(state)
}

func (m *tokenConnections) Stop() []*tokenSession {
	state, winner := m.beginStop()
	<-state.done
	if !winner {
		return nil
	}
	return state.sessions
}

func (m *tokenConnections) beginStop() (*tokenStopState, bool) {
	m.mu.Lock()
	if m.stopState != nil {
		state := m.stopState
		m.mu.Unlock()
		return state, false
	}
	m.stopped = true
	state := &tokenStopState{
		done:          make(chan struct{}),
		active:        make([]*tokenSession, 0, len(m.sessions)),
		ownedRemovals: make([]*tokenRemoveState, 0, len(m.sessions)),
		disableDone:   make([]<-chan struct{}, 0, len(m.disableStates)),
	}
	m.stopState = state
	for session := range m.sessions {
		state.active = append(state.active, session)
		removal, created := m.registerRemoveLocked(session)
		if created {
			state.ownedRemovals = append(state.ownedRemovals, removal)
		}
	}
	for _, disable := range m.disableStates {
		select {
		case <-disable.done:
		default:
			state.disableDone = append(state.disableDone, disable.done)
		}
	}
	for _, expiry := range m.expiryTimers {
		expiry.timer.Stop()
	}
	state.sessions = make([]*tokenSession, 0, len(m.removals))
	state.removals = make([]*tokenRemoveState, 0, len(m.removals))
	for session, removal := range m.removals {
		state.sessions = append(state.sessions, session)
		state.removals = append(state.removals, removal)
	}
	m.sessions = make(map[*tokenSession]struct{})
	m.sessionsByID = make(map[string]map[*tokenSession]struct{})
	m.expiryTimers = make(map[string]tokenExpiryTimer)
	m.mu.Unlock()
	go m.completeStop(state)
	return state, true
}

func (m *tokenConnections) completeStop(state *tokenStopState) {
	waitForTokenSessions(state.active)
	if len(state.active) > 0 && m.onDisabled != nil {
		m.onDisabled(state.active)
	}
	for _, removal := range state.ownedRemovals {
		completeTokenRemove(removal)
	}
	for _, removal := range state.removals {
		<-removal.done
	}
	for _, done := range state.disableDone {
		<-done
	}
	m.expiryWG.Wait()
	m.removeWG.Wait()
	close(state.done)
}

func (m *tokenConnections) beginDisableLocked(tokenID string) (*tokenDisableState, bool) {
	if state := m.disableStates[tokenID]; state != nil {
		return state, false
	}
	state := &tokenDisableState{done: make(chan struct{})}
	m.disableStates[tokenID] = state
	if expiry, ok := m.expiryTimers[tokenID]; ok {
		expiry.timer.Stop()
		delete(m.expiryTimers, tokenID)
	}

	byID := m.sessionsByID[tokenID]
	removing := m.removalsByID[tokenID]
	unique := make(map[*tokenSession]struct{}, len(byID)+len(removing))
	state.sessions = make([]*tokenSession, 0, len(byID)+len(removing))
	state.removals = make([]*tokenRemoveState, 0, len(byID)+len(removing))
	for session, removal := range removing {
		unique[session] = struct{}{}
		state.sessions = append(state.sessions, session)
		state.removals = append(state.removals, removal)
	}
	for session := range byID {
		if _, exists := unique[session]; !exists {
			unique[session] = struct{}{}
			state.sessions = append(state.sessions, session)
		}
		delete(m.sessions, session)
		removal, created := m.registerRemoveLocked(session)
		state.removals = append(state.removals, removal)
		if created {
			state.ownedRemovals = append(state.ownedRemovals, removal)
		}
	}
	delete(m.sessionsByID, tokenID)
	return state, true
}

func (m *tokenConnections) completeDisable(state *tokenDisableState) []*tokenSession {
	sessions := state.sessions
	waitForTokenSessions(sessions)
	if len(sessions) > 0 && m.onDisabled != nil {
		m.onDisabled(sessions)
	}
	for _, removal := range state.ownedRemovals {
		completeTokenRemove(removal)
	}
	for _, removal := range state.removals {
		<-removal.done
	}
	m.mu.Lock()
	state.sessions = nil
	state.removals = nil
	state.ownedRemovals = nil
	m.mu.Unlock()
	close(state.done)
	return sessions
}

func (m *tokenConnections) registerRemoveLocked(session *tokenSession) (*tokenRemoveState, bool) {
	if state := m.removals[session]; state != nil {
		return state, false
	}
	state := &tokenRemoveState{
		session:     session,
		drained:     make(chan struct{}),
		cleanupDone: make(chan struct{}),
		done:        make(chan struct{}),
	}
	m.removals[session] = state
	byID := m.removalsByID[session.principal.ID]
	if byID == nil {
		byID = make(map[*tokenSession]*tokenRemoveState)
		m.removalsByID[session.principal.ID] = byID
	}
	byID[session] = state
	m.removeWG.Add(1)
	go m.finishRemove(state)
	return state, true
}

func (m *tokenConnections) finishRemove(state *tokenRemoveState) {
	defer m.removeWG.Done()
	waitForTokenSessions([]*tokenSession{state.session})
	close(state.drained)
	<-state.cleanupDone
	m.mu.Lock()
	if m.removals[state.session] == state {
		delete(m.removals, state.session)
		byID := m.removalsByID[state.session.principal.ID]
		delete(byID, state.session)
		if len(byID) == 0 {
			delete(m.removalsByID, state.session.principal.ID)
		}
	}
	m.mu.Unlock()
	close(state.done)
}

func completeTokenRemove(state *tokenRemoveState) {
	if state == nil {
		return
	}
	state.complete.Do(func() {
		close(state.cleanupDone)
	})
}

func (m *tokenConnections) detachSessionLocked(session *tokenSession) {
	delete(m.sessions, session)
	byID := m.sessionsByID[session.principal.ID]
	delete(byID, session)
	if len(byID) == 0 {
		delete(m.sessionsByID, session.principal.ID)
	}
}

func (m *tokenConnections) scheduleExpiryLocked(tokenID string, now time.Time) {
	byID := m.sessionsByID[tokenID]
	var earliest *time.Time
	for session := range byID {
		if session.principal.ExpiresAt == nil {
			continue
		}
		if earliest == nil || session.principal.ExpiresAt.Before(*earliest) {
			expiresAt := *session.principal.ExpiresAt
			earliest = &expiresAt
		}
	}

	current, scheduled := m.expiryTimers[tokenID]
	if earliest == nil {
		if scheduled {
			current.timer.Stop()
			delete(m.expiryTimers, tokenID)
		}
		return
	}
	if scheduled && current.expiresAt.Equal(*earliest) {
		return
	}
	if scheduled {
		current.timer.Stop()
	}

	m.nextTimerID++
	generation := m.nextTimerID
	expiresAt := *earliest
	timer := m.afterFunc(expiresAt.Sub(now), func() {
		m.expireToken(tokenID, generation)
	})
	m.expiryTimers[tokenID] = tokenExpiryTimer{
		timer:      timer,
		expiresAt:  expiresAt,
		generation: generation,
	}
}

func (m *tokenConnections) expireToken(tokenID string, generation uint64) {
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return
	}
	m.expiryWG.Add(1)
	m.mu.Unlock()
	defer m.expiryWG.Done()

	now := m.now()
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return
	}
	expiry, scheduled := m.expiryTimers[tokenID]
	if !scheduled || expiry.generation != generation {
		m.mu.Unlock()
		return
	}
	if expiry.expiresAt.After(now) {
		delete(m.expiryTimers, tokenID)
		m.scheduleExpiryLocked(tokenID, now)
		m.mu.Unlock()
		return
	}
	state, winner := m.beginDisableLocked(tokenID)
	m.mu.Unlock()
	if !winner {
		<-state.done
		return
	}
	m.completeDisable(state)
}

func waitForTokenSessions(sessions []*tokenSession) {
	for _, session := range sessions {
		session.gate.Lock()
		session.gate.Unlock()
	}
}
