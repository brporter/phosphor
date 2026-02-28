package cli

import (
	"context"
	"fmt"

	"github.com/coder/websocket"
	"github.com/brporter/phosphor/internal/protocol"
)

// WSConn wraps a WebSocket connection with protocol encoding/decoding.
type WSConn struct {
	conn *websocket.Conn
}

// ConnectWebSocket dials the relay server.
func ConnectWebSocket(ctx context.Context, url string) (*WSConn, error) {
	conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{
		Subprotocols: []string{"phosphor"},
	})
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", url, err)
	}
	conn.SetReadLimit(1 << 20) // 1MB
	return &WSConn{conn: conn}, nil
}

// Send encodes and sends a protocol message.
func (w *WSConn) Send(ctx context.Context, msgType byte, payload any) error {
	data, err := protocol.Encode(msgType, payload)
	if err != nil {
		return err
	}
	return w.conn.Write(ctx, websocket.MessageBinary, data)
}

// Receive reads and decodes a protocol message.
func (w *WSConn) Receive(ctx context.Context) (byte, []byte, error) {
	_, data, err := w.conn.Read(ctx)
	if err != nil {
		return 0, nil, err
	}
	return protocol.Decode(data)
}

// Close closes the underlying connection.
func (w *WSConn) Close() error {
	return w.conn.Close(websocket.StatusNormalClosure, "goodbye")
}
