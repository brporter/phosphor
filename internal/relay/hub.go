package relay

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/brporter/phosphor/internal/protocol"
)

// Hub coordinates between SessionStore, MessageBus, and local sessions.
type Hub struct {
	store   SessionStore
	bus     MessageBus // nil in single-instance mode
	relayID string
	logger  *slog.Logger

	mu     sync.RWMutex
	locals map[string]*LocalSession
}

// NewHub creates a new session hub.
func NewHub(store SessionStore, bus MessageBus, relayID string, logger *slog.Logger) *Hub {
	return &Hub{
		store:   store,
		bus:     bus,
		relayID: relayID,
		logger:  logger,
		locals:  make(map[string]*LocalSession),
	}
}

// Register stores session metadata and creates a local session for the CLI connection.
func (h *Hub) Register(ctx context.Context, info SessionInfo, cliConn *websocket.Conn) (*LocalSession, error) {
	info.RelayID = h.relayID
	if err := h.store.Register(ctx, info); err != nil {
		return nil, err
	}

	ls := NewLocalSession(info.ID, cliConn, h.bus, h.logger)
	ls.SubscribeToInput(ctx)

	h.mu.Lock()
	h.locals[info.ID] = ls
	h.mu.Unlock()

	h.logger.Info("session registered", "id", info.ID, "owner", info.OwnerSub)
	return ls, nil
}

// Unregister removes a session from the store and local map, closing the local session.
func (h *Hub) Unregister(ctx context.Context, sessionID string) {
	h.store.Unregister(ctx, sessionID)

	h.mu.Lock()
	ls, ok := h.locals[sessionID]
	if ok {
		delete(h.locals, sessionID)
	}
	h.mu.Unlock()

	if ok {
		ls.Close()
		h.logger.Info("session unregistered", "id", sessionID)
	}
}

// Get returns session metadata by ID from the store.
func (h *Hub) Get(ctx context.Context, sessionID string) (SessionInfo, bool, error) {
	return h.store.Get(ctx, sessionID)
}

// GetLocal returns the local session if it exists on this relay instance.
func (h *Hub) GetLocal(sessionID string) (*LocalSession, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	ls, ok := h.locals[sessionID]
	return ls, ok
}

// GetOrCreateViewerLocal returns or creates a viewer-only local session.
// If the session's CLI is on a different relay, this creates a viewer-only
// local session and subscribes to the output channel.
func (h *Hub) GetOrCreateViewerLocal(ctx context.Context, sessionID string) (*LocalSession, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if ls, ok := h.locals[sessionID]; ok {
		return ls, nil
	}

	ls := NewViewerOnlyLocalSession(sessionID, h.bus, h.logger)
	ls.SubscribeToOutput(ctx)
	h.locals[sessionID] = ls
	return ls, nil
}

// ListForOwner returns all sessions owned by the given identity from the store.
func (h *Hub) ListForOwner(ctx context.Context, provider, sub string) ([]SessionInfo, error) {
	return h.store.ListForOwner(ctx, provider, sub)
}

// Disconnect marks a session as disconnected, broadcasts to viewers, and schedules expiry.
func (h *Hub) Disconnect(ctx context.Context, sessionID string, gracePeriod time.Duration) {
	h.store.SetDisconnected(ctx, sessionID)
	h.logger.Info("cli disconnected, grace period started", "id", sessionID, "grace", gracePeriod)

	// Broadcast reconnect status to local viewers
	data, err := protocol.Encode(protocol.TypeReconnect, protocol.Reconnect{Status: "disconnected"})
	if err == nil {
		if ls, ok := h.GetLocal(sessionID); ok {
			ls.BroadcastToLocalViewers(ctx, data)
		}
		// Also publish to bus for remote viewers
		if h.bus != nil {
			h.bus.Publish(ctx, OutputChannel(sessionID), data)
		}
	}

	h.store.ScheduleExpiry(ctx, sessionID, gracePeriod)
}

// Reconnect cancels expiry, updates store, and replaces the CLI connection.
func (h *Hub) Reconnect(ctx context.Context, sessionID string, conn *websocket.Conn, newToken string) error {
	h.store.CancelExpiry(ctx, sessionID)
	h.store.SetReconnected(ctx, sessionID, newToken, h.relayID)

	h.mu.Lock()
	ls, ok := h.locals[sessionID]
	if !ok {
		// CLI reconnecting to a different relay — create new local session
		ls = NewLocalSession(sessionID, conn, h.bus, h.logger)
		ls.SubscribeToInput(ctx)
		h.locals[sessionID] = ls
	}
	h.mu.Unlock()

	if ok {
		ls.ReplaceCLI(conn)
	}

	// Broadcast reconnect status to local viewers
	data, err := protocol.Encode(protocol.TypeReconnect, protocol.Reconnect{Status: "reconnected"})
	if err == nil {
		ls.BroadcastToLocalViewers(ctx, data)
		if h.bus != nil {
			h.bus.Publish(ctx, OutputChannel(sessionID), data)
		}
	}

	h.logger.Info("cli reconnected", "id", sessionID)
	return nil
}

// BroadcastOutput writes data to local viewers and publishes to the bus.
func (h *Hub) BroadcastOutput(ctx context.Context, sessionID string, data []byte) {
	if ls, ok := h.GetLocal(sessionID); ok {
		ls.BroadcastToLocalViewers(ctx, data)
	}
	if h.bus != nil {
		h.bus.Publish(ctx, OutputChannel(sessionID), data)
	}
}

// SendInput writes data to the local CLI, or publishes to the input bus if CLI is remote.
func (h *Hub) SendInput(ctx context.Context, sessionID string, data []byte) error {
	if ls, ok := h.GetLocal(sessionID); ok && ls.HasCLI() {
		return ls.SendToCLI(ctx, data)
	}
	if h.bus != nil {
		return h.bus.Publish(ctx, InputChannel(sessionID), data)
	}
	return nil
}

// CleanupViewerLocal removes viewer-only local sessions with zero viewers.
func (h *Hub) CleanupViewerLocal(sessionID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	ls, ok := h.locals[sessionID]
	if !ok {
		return
	}
	if !ls.HasCLI() && ls.ViewerCount() == 0 {
		ls.Close()
		delete(h.locals, sessionID)
	}
}

// CloseAll shuts down all local sessions (used during server shutdown).
func (h *Hub) CloseAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, ls := range h.locals {
		ls.Close()
	}
	h.locals = make(map[string]*LocalSession)
}
