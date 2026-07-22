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

	gate        sync.RWMutex
	sendGate    sync.Mutex
	leases      atomic.Int64
	messageGate sync.Mutex
	uid         string
	admitted    bool
	terminal    bool
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

type tokenConnections struct {
	mu        sync.Mutex
	now       func() time.Time
	afterFunc connectionAfterFunc
	onExpired func([]*tokenSession)

	sessions     map[*tokenSession]struct{}
	sessionsByID map[string]map[*tokenSession]struct{}
	drainingByID map[string]map[*tokenSession]struct{}
	disabledIDs  map[string]struct{}
	expiryTimers map[string]tokenExpiryTimer
	nextTimerID  uint64
	stopped      bool
}

func newTokenConnections(
	now func() time.Time,
	afterFunc connectionAfterFunc,
	onExpired func([]*tokenSession),
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
		now:          now,
		afterFunc:    afterFunc,
		onExpired:    onExpired,
		sessions:     make(map[*tokenSession]struct{}),
		sessionsByID: make(map[string]map[*tokenSession]struct{}),
		drainingByID: make(map[string]map[*tokenSession]struct{}),
		disabledIDs:  make(map[string]struct{}),
		expiryTimers: make(map[string]tokenExpiryTimer),
	}
}

func (m *tokenConnections) Track(session *tokenSession, conn types.Connection) bool {
	if session == nil || conn == nil || session.principal.ID == "" {
		return false
	}

	now := m.now()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopped {
		return false
	}
	if _, disabled := m.disabledIDs[session.principal.ID]; disabled {
		return false
	}
	if session.principal.ExpiresAt != nil && !session.principal.ExpiresAt.After(now) {
		m.disabledIDs[session.principal.ID] = struct{}{}
		return false
	}
	if _, tracked := m.sessions[session]; tracked {
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
	if _, disabled := m.disabledIDs[session.principal.ID]; disabled {
		m.mu.Unlock()
		return nil, false
	}
	if _, tracked := m.sessions[session]; !tracked {
		m.mu.Unlock()
		return nil, false
	}
	if session.principal.ExpiresAt != nil && !session.principal.ExpiresAt.After(now) {
		sessions := m.disableLocked(session.principal.ID)
		m.mu.Unlock()
		waitForTokenSessions(sessions)
		m.notifyExpired(sessions)
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
	if _, disabled := m.disabledIDs[tokenID]; disabled {
		m.mu.Unlock()
		return nil
	}
	sessions := m.disableLocked(tokenID)
	m.mu.Unlock()

	waitForTokenSessions(sessions)
	return sessions
}

func (m *tokenConnections) Stop() []*tokenSession {
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return nil
	}
	m.stopped = true
	sessions := make([]*tokenSession, 0, len(m.sessions))
	for session := range m.sessions {
		sessions = append(sessions, session)
	}
	for _, expiry := range m.expiryTimers {
		expiry.timer.Stop()
	}
	m.sessions = make(map[*tokenSession]struct{})
	m.sessionsByID = make(map[string]map[*tokenSession]struct{})
	m.drainingByID = make(map[string]map[*tokenSession]struct{})
	m.expiryTimers = make(map[string]tokenExpiryTimer)
	m.mu.Unlock()
	return sessions
}

func (m *tokenConnections) disableLocked(tokenID string) []*tokenSession {
	m.disabledIDs[tokenID] = struct{}{}
	if expiry, ok := m.expiryTimers[tokenID]; ok {
		expiry.timer.Stop()
		delete(m.expiryTimers, tokenID)
	}

	byID := m.sessionsByID[tokenID]
	draining := m.drainingByID[tokenID]
	sessions := make([]*tokenSession, 0, len(byID)+len(draining))
	for session := range byID {
		sessions = append(sessions, session)
		delete(m.sessions, session)
	}
	for session := range draining {
		sessions = append(sessions, session)
	}
	delete(m.sessionsByID, tokenID)
	delete(m.drainingByID, tokenID)
	return sessions
}

func (m *tokenConnections) finishDrain(session *tokenSession) {
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
	sessions := m.disableLocked(tokenID)
	m.mu.Unlock()

	waitForTokenSessions(sessions)
	m.notifyExpired(sessions)
}

func (m *tokenConnections) notifyExpired(sessions []*tokenSession) {
	if len(sessions) > 0 && m.onExpired != nil {
		m.onExpired(sessions)
	}
}

func waitForTokenSessions(sessions []*tokenSession) {
	for _, session := range sessions {
		session.gate.Lock()
		session.gate.Unlock()
	}
}
