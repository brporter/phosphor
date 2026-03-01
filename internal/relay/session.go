package relay

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/brporter/phosphor/internal/protocol"
)

const maxViewersPerSession = 10

// Session represents an active terminal sharing session.
type Session struct {
	ID            string
	OwnerProvider string
	OwnerSub      string
	Mode          string // "pty" or "pipe"
	Cols          int
	Rows          int
	Command       string

	ReconnectToken string

	cliConn *websocket.Conn
	logger  *slog.Logger

	mu              sync.RWMutex
	viewers         map[string]*websocket.Conn // viewer ID → conn
	closed          bool
	cliDisconnected bool
	disconnectedAt  time.Time
}

// NewSession creates a new session.
func NewSession(id, ownerProvider, ownerSub string, cliConn *websocket.Conn, hello protocol.Hello, logger *slog.Logger) *Session {
	return &Session{
		ID:             id,
		OwnerProvider:  ownerProvider,
		OwnerSub:       ownerSub,
		Mode:           hello.Mode,
		Cols:           hello.Cols,
		Rows:           hello.Rows,
		Command:        hello.Command,
		ReconnectToken: generateToken(),
		cliConn:        cliConn,
		logger:         logger,
		viewers:        make(map[string]*websocket.Conn),
	}
}

func generateToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// MarkDisconnected marks the CLI as disconnected and notifies viewers.
func (s *Session) MarkDisconnected() {
	s.mu.Lock()
	s.cliDisconnected = true
	s.disconnectedAt = time.Now()
	s.mu.Unlock()

	s.BroadcastToViewers(context.Background(), protocol.TypeReconnect, protocol.Reconnect{Status: "disconnected"})
}

// ReplaceCLI swaps the CLI connection (used on reconnect).
func (s *Session) ReplaceCLI(conn *websocket.Conn) {
	s.mu.Lock()
	s.cliConn = conn
	s.cliDisconnected = false
	s.disconnectedAt = time.Time{}
	s.mu.Unlock()

	s.BroadcastToViewers(context.Background(), protocol.TypeReconnect, protocol.Reconnect{Status: "reconnected"})
}

// IsDisconnected returns true if the CLI is currently disconnected.
func (s *Session) IsDisconnected() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cliDisconnected
}

// RotateReconnectToken generates a new reconnect token.
func (s *Session) RotateReconnectToken() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ReconnectToken = generateToken()
}

// AddViewer adds a viewer connection. Returns false if limit reached.
func (s *Session) AddViewer(id string, conn *websocket.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || len(s.viewers) >= maxViewersPerSession {
		return false
	}
	s.viewers[id] = conn
	return true
}

// RemoveViewer removes a viewer connection.
func (s *Session) RemoveViewer(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.viewers, id)
}

// ViewerCount returns the number of connected viewers.
func (s *Session) ViewerCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.viewers)
}

// BroadcastToViewers sends a message to all viewers.
func (s *Session) BroadcastToViewers(ctx context.Context, msgType byte, payload any) {
	data, err := protocol.Encode(msgType, payload)
	if err != nil {
		s.logger.Error("encode broadcast", "err", err)
		return
	}

	s.mu.RLock()
	viewers := make(map[string]*websocket.Conn, len(s.viewers))
	for k, v := range s.viewers {
		viewers[k] = v
	}
	s.mu.RUnlock()

	for id, conn := range viewers {
		if err := conn.Write(ctx, websocket.MessageBinary, data); err != nil {
			s.logger.Debug("viewer write failed", "viewer", id, "err", err)
		}
	}
}

// SendToCLI sends a message to the CLI connection.
func (s *Session) SendToCLI(ctx context.Context, msgType byte, payload any) error {
	s.mu.RLock()
	if s.cliDisconnected {
		s.mu.RUnlock()
		return fmt.Errorf("cli disconnected")
	}
	conn := s.cliConn
	s.mu.RUnlock()

	data, err := protocol.Encode(msgType, payload)
	if err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageBinary, data)
}

// NotifyViewerCount sends the current viewer count to the CLI.
func (s *Session) NotifyViewerCount(ctx context.Context) {
	s.SendToCLI(ctx, protocol.TypeViewerCount, protocol.ViewerCount{Count: s.ViewerCount()})
}

// Close notifies all viewers and marks the session as closed.
func (s *Session) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true

	// Notify viewers of session end
	endMsg, _ := protocol.Encode(protocol.TypeEnd, nil)
	for _, conn := range s.viewers {
		conn.Write(context.Background(), websocket.MessageBinary, endMsg)
		conn.Close(websocket.StatusNormalClosure, "session ended")
	}
	s.viewers = make(map[string]*websocket.Conn)
}
