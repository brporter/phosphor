package cli

import (
	"context"
	"sync"
	"time"

	"github.com/brporter/phosphor/internal/protocol"
)

// wsNotifier provides a thread-safe way for goroutines (stdin reader, resize
// watcher) to send messages to the relay via the current WebSocket connection.
// The active connection is set/cleared by the reconnection loop.
type wsNotifier struct {
	mu            sync.Mutex
	ws            *WSConn
	ctx           context.Context
	lastInputNote time.Time
}

// Set configures the active WebSocket connection and context.
func (n *wsNotifier) Set(ws *WSConn, ctx context.Context) {
	n.mu.Lock()
	n.ws = ws
	n.ctx = ctx
	n.mu.Unlock()
}

// Clear removes the active connection (e.g. on disconnect).
func (n *wsNotifier) Clear() {
	n.mu.Lock()
	n.ws = nil
	n.ctx = nil
	n.mu.Unlock()
}

// SendResize sends a TypeResize message to the relay with the current
// local terminal dimensions.
func (n *wsNotifier) SendResize(cols, rows int) {
	n.mu.Lock()
	ws := n.ws
	ctx := n.ctx
	n.mu.Unlock()
	if ws != nil && ctx != nil {
		ws.Send(ctx, protocol.TypeResize, protocol.Resize{Cols: cols, Rows: rows})
	}
}

// NotifyLocalInput sends a debounced zero-length TypeStdin to the relay
// so it knows the local terminal is receiving keyboard input.
// At most one notification is sent per second to avoid flooding.
func (n *wsNotifier) NotifyLocalInput() {
	n.mu.Lock()
	ws := n.ws
	ctx := n.ctx
	if ws == nil || ctx == nil || time.Since(n.lastInputNote) < time.Second {
		n.mu.Unlock()
		return
	}
	n.lastInputNote = time.Now()
	n.mu.Unlock()
	ws.Send(ctx, protocol.TypeStdin, []byte{})
}
