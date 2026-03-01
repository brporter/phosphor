package relay

import (
	"log/slog"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Hub manages all active sessions and routes messages.
type Hub struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	logger   *slog.Logger
}

// NewHub creates a new session hub.
func NewHub(logger *slog.Logger) *Hub {
	return &Hub{
		sessions: make(map[string]*Session),
		logger:   logger,
	}
}

// Register adds a session to the hub.
func (h *Hub) Register(s *Session) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sessions[s.ID] = s
	h.logger.Info("session registered", "id", s.ID, "owner", s.OwnerSub)
}

// Unregister removes a session from the hub and notifies viewers.
func (h *Hub) Unregister(sessionID string) {
	h.mu.Lock()
	s, ok := h.sessions[sessionID]
	if ok {
		delete(h.sessions, sessionID)
	}
	h.mu.Unlock()

	if ok {
		s.Close()
		h.logger.Info("session unregistered", "id", sessionID)
	}
}

// Get returns a session by ID.
func (h *Hub) Get(sessionID string) (*Session, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	s, ok := h.sessions[sessionID]
	return s, ok
}

// ListForOwner returns all sessions owned by the given identity.
func (h *Hub) ListForOwner(provider, sub string) []*Session {
	h.mu.RLock()
	defer h.mu.RUnlock()
	var result []*Session
	for _, s := range h.sessions {
		if s.OwnerProvider == provider && s.OwnerSub == sub {
			result = append(result, s)
		}
	}
	return result
}

// Disconnect marks a session's CLI as disconnected and schedules cleanup after the grace period.
func (h *Hub) Disconnect(sessionID string, gracePeriod time.Duration) {
	h.mu.RLock()
	s, ok := h.sessions[sessionID]
	h.mu.RUnlock()
	if !ok {
		return
	}

	s.MarkDisconnected()
	h.logger.Info("cli disconnected, grace period started", "id", sessionID, "grace", gracePeriod)

	go func() {
		time.Sleep(gracePeriod)
		if s.IsDisconnected() {
			h.Unregister(sessionID)
			h.logger.Info("grace period expired, session removed", "id", sessionID)
		}
	}()
}

// Reconnect replaces the CLI connection on a disconnected session.
func (h *Hub) Reconnect(sessionID string, conn *websocket.Conn) bool {
	h.mu.RLock()
	s, ok := h.sessions[sessionID]
	h.mu.RUnlock()
	if !ok || !s.IsDisconnected() {
		return false
	}

	s.ReplaceCLI(conn)
	h.logger.Info("cli reconnected", "id", sessionID)
	return true
}

// CloseAll shuts down all sessions (used during server shutdown).
func (h *Hub) CloseAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, s := range h.sessions {
		s.Close()
	}
	h.sessions = make(map[string]*Session)
}
