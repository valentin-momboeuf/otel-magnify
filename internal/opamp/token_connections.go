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

	gate           sync.RWMutex
	sendGate       sync.Mutex
	leases         atomic.Int64
	messageGate    sync.Mutex
	httpMu         sync.Mutex
	httpLease      *tokenLease
	disconnectOnce sync.Once
	uid            string
	admitted       bool
	terminal       bool
}

func (s *tokenSession) holdHTTPLease(lease *tokenLease) bool {
	s.httpMu.Lock()
	defer s.httpMu.Unlock()
	if s.httpLease != nil {
		return false
	}
	s.httpLease = lease
	return true
}

func (s *tokenSession) finishHTTPResponse() {
	s.httpMu.Lock()
	lease := s.httpLease
	s.httpLease = nil
	s.httpMu.Unlock()
	if lease == nil {
		return
	}
	s.clearWriteDeadline()
	lease.Release()
}

func (s *tokenSession) clearWriteDeadline() {
	if conn := s.conn.Connection(); conn != nil {
		if err := conn.SetWriteDeadline(time.Time{}); err != nil {
			_ = conn.Close()
		}
	}
}

type tokenLease struct {
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

type tokenExpiryTimer struct {
	timer      connectionTimer
	expiresAt  time.Time
	generation uint64
}

type tokenDisableState struct {
	done     chan struct{}
	sessions []*tokenSession
}

type tokenConnections struct {
	mu         sync.Mutex
	now        func() time.Time
	afterFunc  connectionAfterFunc
	onDisabled func([]*tokenSession)

	sessions      map[*tokenSession]struct{}
	sessionsByID  map[string]map[*tokenSession]struct{}
	drainingByID  map[string]map[*tokenSession]struct{}
	disableStates map[string]*tokenDisableState
	expiryTimers  map[string]tokenExpiryTimer
	nextTimerID   uint64
	stopped       bool
	stopDone      chan struct{}
	expiryWG      sync.WaitGroup
	drainWG       sync.WaitGroup
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
		drainingByID:  make(map[string]map[*tokenSession]struct{}),
		disableStates: make(map[string]*tokenDisableState),
		expiryTimers:  make(map[string]tokenExpiryTimer),
		stopDone:      make(chan struct{}),
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
		state, _ := m.beginDisableLocked(session.principal.ID)
		m.expiryWG.Add(1)
		m.mu.Unlock()
		m.completeDisable(state)
		m.expiryWG.Done()
		return nil, false
	}

	// The global mutex fixes the admission order relative to Disable. Once this
	// read gate is held, a disabling caller must wait for the operation to end.
	session.gate.RLock()
	session.leases.Add(1)
	m.mu.Unlock()
	return &tokenLease{session: session}, true
}

func (m *tokenConnections) Remove(session *tokenSession) {
	if session == nil {
		return
	}
	m.mu.Lock()
	if _, tracked := m.sessions[session]; !tracked {
		m.mu.Unlock()
		return
	}
	m.detachSessionLocked(session)
	needsDrain := session.leases.Load() > 0
	if needsDrain {
		m.drainWG.Add(1)
		draining := m.drainingByID[session.principal.ID]
		if draining == nil {
			draining = make(map[*tokenSession]struct{})
			m.drainingByID[session.principal.ID] = draining
		}
		draining[session] = struct{}{}
	}
	m.scheduleExpiryLocked(session.principal.ID, m.now())
	m.mu.Unlock()

	if needsDrain {
		go m.finishDrain(session)
	}
}

func (m *tokenConnections) Disable(tokenID string) []*tokenSession {
	m.mu.Lock()
	if m.stopped {
		stopDone := m.stopDone
		m.mu.Unlock()
		<-stopDone
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
	m.mu.Lock()
	if m.stopped {
		stopDone := m.stopDone
		m.mu.Unlock()
		<-stopDone
		return nil
	}
	m.stopped = true
	unique := make(map[*tokenSession]struct{}, len(m.sessions))
	for session := range m.sessions {
		unique[session] = struct{}{}
	}
	for _, draining := range m.drainingByID {
		for session := range draining {
			unique[session] = struct{}{}
		}
	}
	disableDone := make([]<-chan struct{}, 0, len(m.disableStates))
	for _, state := range m.disableStates {
		select {
		case <-state.done:
		default:
			disableDone = append(disableDone, state.done)
		}
	}
	for _, expiry := range m.expiryTimers {
		expiry.timer.Stop()
	}
	sessions := make([]*tokenSession, 0, len(unique))
	for session := range unique {
		sessions = append(sessions, session)
	}
	m.sessions = make(map[*tokenSession]struct{})
	m.sessionsByID = make(map[string]map[*tokenSession]struct{})
	m.drainingByID = make(map[string]map[*tokenSession]struct{})
	m.expiryTimers = make(map[string]tokenExpiryTimer)
	m.mu.Unlock()

	waitForTokenSessions(sessions)
	for _, done := range disableDone {
		<-done
	}
	m.expiryWG.Wait()
	m.drainWG.Wait()
	close(m.stopDone)
	return sessions
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
	draining := m.drainingByID[tokenID]
	state.sessions = make([]*tokenSession, 0, len(byID)+len(draining))
	for session := range byID {
		state.sessions = append(state.sessions, session)
		delete(m.sessions, session)
	}
	for session := range draining {
		state.sessions = append(state.sessions, session)
	}
	delete(m.sessionsByID, tokenID)
	delete(m.drainingByID, tokenID)
	return state, true
}

func (m *tokenConnections) completeDisable(state *tokenDisableState) []*tokenSession {
	sessions := state.sessions
	waitForTokenSessions(sessions)
	if len(sessions) > 0 && m.onDisabled != nil {
		m.onDisabled(sessions)
	}
	m.mu.Lock()
	state.sessions = nil
	m.mu.Unlock()
	close(state.done)
	return sessions
}

func (m *tokenConnections) finishDrain(session *tokenSession) {
	defer m.drainWG.Done()
	waitForTokenSessions([]*tokenSession{session})
	m.mu.Lock()
	draining := m.drainingByID[session.principal.ID]
	delete(draining, session)
	if len(draining) == 0 {
		delete(m.drainingByID, session.principal.ID)
	}
	m.mu.Unlock()
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
	now := m.now()
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return
	}
	m.expiryWG.Add(1)
	defer m.expiryWG.Done()
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
