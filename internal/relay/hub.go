package relay

import (
	"log/slog"
	"sync"
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

// CloseAll shuts down all sessions (used during server shutdown).
func (h *Hub) CloseAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, s := range h.sessions {
		s.Close()
	}
	h.sessions = make(map[string]*Session)
}
