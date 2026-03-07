package relay

import (
	"context"
	"sync"
	"time"
)

// ExpiryCallback is called when a session's grace period expires.
type ExpiryCallback func(ctx context.Context, sessionID string)

// MemorySessionStore is an in-memory implementation of SessionStore.
type MemorySessionStore struct {
	mu       sync.RWMutex
	sessions map[string]SessionInfo
	timers   map[string]*time.Timer
	onExpiry ExpiryCallback
}

// NewMemorySessionStore creates a new in-memory session store.
func NewMemorySessionStore() *MemorySessionStore {
	return &MemorySessionStore{
		sessions: make(map[string]SessionInfo),
		timers:   make(map[string]*time.Timer),
	}
}

// SetExpiryCallback sets the function called when a session's grace period expires.
func (m *MemorySessionStore) SetExpiryCallback(fn ExpiryCallback) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onExpiry = fn
}

func (m *MemorySessionStore) Register(_ context.Context, info SessionInfo) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[info.ID] = info
	return nil
}

func (m *MemorySessionStore) Unregister(_ context.Context, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, sessionID)
	if t, ok := m.timers[sessionID]; ok {
		t.Stop()
		delete(m.timers, sessionID)
	}
	return nil
}

func (m *MemorySessionStore) Get(_ context.Context, sessionID string) (SessionInfo, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	info, ok := m.sessions[sessionID]
	return info, ok, nil
}

func (m *MemorySessionStore) ListForOwner(_ context.Context, provider, sub string) ([]SessionInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []SessionInfo
	for _, info := range m.sessions {
		if info.OwnerProvider == provider && info.OwnerSub == sub {
			result = append(result, info)
		}
	}
	return result, nil
}

func (m *MemorySessionStore) SetDisconnected(_ context.Context, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	info, ok := m.sessions[sessionID]
	if !ok {
		return nil
	}
	info.Disconnected = true
	info.DisconnectedAt = time.Now()
	m.sessions[sessionID] = info
	return nil
}

func (m *MemorySessionStore) SetReconnected(_ context.Context, sessionID, newToken, relayID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	info, ok := m.sessions[sessionID]
	if !ok {
		return nil
	}
	info.Disconnected = false
	info.DisconnectedAt = time.Time{}
	info.ReconnectToken = newToken
	info.RelayID = relayID
	m.sessions[sessionID] = info
	return nil
}

func (m *MemorySessionStore) UpdateDimensions(_ context.Context, sessionID string, cols, rows int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	info, ok := m.sessions[sessionID]
	if !ok {
		return nil
	}
	info.Cols = cols
	info.Rows = rows
	m.sessions[sessionID] = info
	return nil
}

func (m *MemorySessionStore) ScheduleExpiry(_ context.Context, sessionID string, grace time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if t, ok := m.timers[sessionID]; ok {
		t.Stop()
	}

	m.timers[sessionID] = time.AfterFunc(grace, func() {
		m.mu.Lock()
		delete(m.timers, sessionID)
		cb := m.onExpiry
		m.mu.Unlock()

		if cb != nil {
			cb(context.Background(), sessionID)
		}
	})
	return nil
}

func (m *MemorySessionStore) CancelExpiry(_ context.Context, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.timers[sessionID]; ok {
		t.Stop()
		delete(m.timers, sessionID)
	}
	return nil
}

func (m *MemorySessionStore) SetProcessExited(_ context.Context, sessionID string, exited bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	info, ok := m.sessions[sessionID]
	if !ok {
		return nil
	}
	info.ProcessExited = exited
	m.sessions[sessionID] = info
	return nil
}

func (m *MemorySessionStore) SetProcessRunning(_ context.Context, sessionID string, running bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	info, ok := m.sessions[sessionID]
	if !ok {
		return nil
	}
	info.ProcessRunning = running
	m.sessions[sessionID] = info
	return nil
}
