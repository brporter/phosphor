package relay

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/coder/websocket"
	"github.com/brporter/phosphor/internal/protocol"
)

const maxViewersPerSession = 10

// LocalSession holds WebSocket connections local to one relay instance.
type LocalSession struct {
	sessionID string
	bus       MessageBus // nil in single-instance mode
	logger    *slog.Logger

	mu      sync.RWMutex
	cliConn *websocket.Conn            // nil for viewer-only sessions
	viewers map[string]*websocket.Conn // viewer ID → conn
	closed  bool

	cancelOutput func() // unsubscribe from output channel
	cancelInput  func() // unsubscribe from input channel
}

// NewLocalSession creates a local session that hosts the CLI connection.
func NewLocalSession(sessionID string, cliConn *websocket.Conn, bus MessageBus, logger *slog.Logger) *LocalSession {
	return &LocalSession{
		sessionID: sessionID,
		cliConn:   cliConn,
		bus:       bus,
		logger:    logger,
		viewers:   make(map[string]*websocket.Conn),
	}
}

// NewViewerOnlyLocalSession creates a local session for a relay that only hosts viewers.
func NewViewerOnlyLocalSession(sessionID string, bus MessageBus, logger *slog.Logger) *LocalSession {
	return &LocalSession{
		sessionID: sessionID,
		bus:       bus,
		logger:    logger,
		viewers:   make(map[string]*websocket.Conn),
	}
}

// HasCLI returns true if this local session hosts the CLI connection.
func (ls *LocalSession) HasCLI() bool {
	ls.mu.RLock()
	defer ls.mu.RUnlock()
	return ls.cliConn != nil
}

// AddViewer adds a viewer connection. Returns false if limit reached or session closed.
func (ls *LocalSession) AddViewer(id string, conn *websocket.Conn) bool {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	if ls.closed || len(ls.viewers) >= maxViewersPerSession {
		return false
	}
	ls.viewers[id] = conn
	return true
}

// RemoveViewer removes a viewer connection.
func (ls *LocalSession) RemoveViewer(id string) {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	delete(ls.viewers, id)
}

// ViewerCount returns the number of connected viewers.
func (ls *LocalSession) ViewerCount() int {
	ls.mu.RLock()
	defer ls.mu.RUnlock()
	return len(ls.viewers)
}

// BroadcastToLocalViewers writes pre-encoded bytes to all local viewers.
func (ls *LocalSession) BroadcastToLocalViewers(ctx context.Context, data []byte) {
	ls.mu.RLock()
	viewers := make(map[string]*websocket.Conn, len(ls.viewers))
	for k, v := range ls.viewers {
		viewers[k] = v
	}
	ls.mu.RUnlock()

	for id, conn := range viewers {
		if err := conn.Write(ctx, websocket.MessageBinary, data); err != nil {
			ls.logger.Debug("viewer write failed", "viewer", id, "err", err)
		}
	}
}

// SendToCLI writes pre-encoded bytes to the local CLI connection.
func (ls *LocalSession) SendToCLI(ctx context.Context, data []byte) error {
	ls.mu.RLock()
	conn := ls.cliConn
	ls.mu.RUnlock()

	if conn == nil {
		return fmt.Errorf("no local CLI connection")
	}
	return conn.Write(ctx, websocket.MessageBinary, data)
}

// ReplaceCLI swaps the CLI connection (used on reconnect).
func (ls *LocalSession) ReplaceCLI(conn *websocket.Conn) {
	ls.mu.Lock()
	ls.cliConn = conn
	ls.mu.Unlock()
}

// SubscribeToOutput subscribes to the bus output channel and forwards to local viewers.
// Used by viewer-only sessions. Runs in a goroutine until cancelled.
func (ls *LocalSession) SubscribeToOutput(ctx context.Context) {
	if ls.bus == nil {
		return
	}
	ch, unsub, err := ls.bus.Subscribe(ctx, OutputChannel(ls.sessionID))
	if err != nil {
		ls.logger.Error("subscribe output failed", "session", ls.sessionID, "err", err)
		return
	}

	ls.mu.Lock()
	ls.cancelOutput = unsub
	ls.mu.Unlock()

	go func() {
		for data := range ch {
			ls.BroadcastToLocalViewers(ctx, data)
		}
	}()
}

// SubscribeToInput subscribes to the bus input channel and forwards to the local CLI.
// Used by CLI-hosting sessions. Runs in a goroutine until cancelled.
func (ls *LocalSession) SubscribeToInput(ctx context.Context) {
	if ls.bus == nil {
		return
	}
	ch, unsub, err := ls.bus.Subscribe(ctx, InputChannel(ls.sessionID))
	if err != nil {
		ls.logger.Error("subscribe input failed", "session", ls.sessionID, "err", err)
		return
	}

	ls.mu.Lock()
	ls.cancelInput = unsub
	ls.mu.Unlock()

	go func() {
		for data := range ch {
			if err := ls.SendToCLI(ctx, data); err != nil {
				ls.logger.Debug("forward input to CLI failed", "session", ls.sessionID, "err", err)
			}
		}
	}()
}

// NotifyViewerCount sends the current viewer count to the CLI.
func (ls *LocalSession) NotifyViewerCount(ctx context.Context) {
	data, err := protocol.Encode(protocol.TypeViewerCount, protocol.ViewerCount{Count: ls.ViewerCount()})
	if err != nil {
		return
	}
	ls.SendToCLI(ctx, data)
}

// Close sends TypeEnd to viewers, closes connections, and unsubscribes from bus.
func (ls *LocalSession) Close() {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	if ls.closed {
		return
	}
	ls.closed = true

	endMsg, _ := protocol.Encode(protocol.TypeEnd, nil)
	for _, conn := range ls.viewers {
		conn.Write(context.Background(), websocket.MessageBinary, endMsg)
		conn.Close(websocket.StatusNormalClosure, "session ended")
	}
	ls.viewers = make(map[string]*websocket.Conn)

	if ls.cancelOutput != nil {
		ls.cancelOutput()
	}
	if ls.cancelInput != nil {
		ls.cancelInput()
	}
}
