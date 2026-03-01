package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/brporter/phosphor/internal/protocol"
)

// newEchoWSServer creates a WS echo server that reads and echoes back messages.
func newEchoWSServer(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			Subprotocols: []string{"phosphor"},
		})
		if err != nil {
			return
		}
		defer conn.CloseNow()
		for {
			typ, data, err := conn.Read(r.Context())
			if err != nil {
				return
			}
			conn.Write(r.Context(), typ, data)
		}
	}))
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

func TestConnectWebSocket_Success(t *testing.T) {
	wsURL := newEchoWSServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := ConnectWebSocket(ctx, wsURL)
	if err != nil {
		t.Fatalf("ConnectWebSocket returned unexpected error: %v", err)
	}
	if conn == nil {
		t.Fatal("ConnectWebSocket returned nil WSConn")
	}
	conn.Close()
}

func TestConnectWebSocket_Failure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := ConnectWebSocket(ctx, "ws://localhost:1")
	if err == nil {
		t.Fatal("expected error connecting to invalid address, got nil")
	}
}

func TestWSConn_SendReceive(t *testing.T) {
	wsURL := newEchoWSServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := ConnectWebSocket(ctx, wsURL)
	if err != nil {
		t.Fatalf("ConnectWebSocket failed: %v", err)
	}
	defer conn.Close()

	if err := conn.Send(ctx, protocol.TypePing, nil); err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	msgType, _, err := conn.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive failed: %v", err)
	}
	if msgType != protocol.TypePing {
		t.Errorf("got message type 0x%02x, want 0x%02x (TypePing)", msgType, protocol.TypePing)
	}
}

func TestWSConn_Close(t *testing.T) {
	wsURL := newEchoWSServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := ConnectWebSocket(ctx, wsURL)
	if err != nil {
		t.Fatalf("ConnectWebSocket failed: %v", err)
	}

	if err := conn.Close(); err != nil {
		t.Errorf("Close returned unexpected error: %v", err)
	}
}
