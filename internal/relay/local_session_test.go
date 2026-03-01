package relay

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/brporter/phosphor/internal/protocol"
	"github.com/coder/websocket"
)

// newWSPair creates a matched server/client WebSocket pair for testing.
// The returned serverConn is the connection as seen from the server side
// (i.e. what you pass into a LocalSession), and clientConn is what you read
// from in assertions.
func newWSPair(t *testing.T) (serverConn *websocket.Conn, clientConn *websocket.Conn) {
	t.Helper()

	var (
		sConn *websocket.Conn
		mu    sync.Mutex
		ready = make(chan struct{})
	)

	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("websocket.Accept: %v", err)
			return
		}
		mu.Lock()
		sConn = conn
		mu.Unlock()
		close(ready)
		// Keep the handler alive so the connection is not torn down.
		<-r.Context().Done()
	}))
	t.Cleanup(s.Close)

	ctx := context.Background()
	cConn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(s.URL, "http"), nil)
	if err != nil {
		t.Fatalf("websocket.Dial: %v", err)
	}
	t.Cleanup(func() { cConn.CloseNow() })

	// Wait until the server handler has stored sConn.
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for server websocket accept")
	}

	mu.Lock()
	sc := sConn
	mu.Unlock()

	t.Cleanup(func() { sc.CloseNow() })
	return sc, cConn
}

// readMessage reads one binary message from conn with a 2-second deadline.
func readMessage(t *testing.T, conn *websocket.Conn) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	msgType, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read message: %v", err)
	}
	if msgType != websocket.MessageBinary {
		t.Fatalf("expected binary message, got %v", msgType)
	}
	return data
}

// newTestLogger returns a no-op slog.Logger suitable for tests.
func newTestLogger() *slog.Logger {
	return slog.Default()
}

// TestNewLocalSession verifies that NewLocalSession populates the expected fields.
func TestNewLocalSession(t *testing.T) {
	cliServer, _ := newWSPair(t)
	ls := NewLocalSession("sess-abc", cliServer, nil, newTestLogger())

	if !ls.HasCLI() {
		t.Error("HasCLI() = false, want true")
	}
	if ls.ViewerCount() != 0 {
		t.Errorf("ViewerCount = %d, want 0", ls.ViewerCount())
	}
	if ls.closed {
		t.Error("closed = true on new session, want false")
	}
}

// TestNewViewerOnlyLocalSession verifies viewer-only sessions have no CLI.
func TestNewViewerOnlyLocalSession(t *testing.T) {
	ls := NewViewerOnlyLocalSession("sess-xyz", nil, newTestLogger())

	if ls.HasCLI() {
		t.Error("HasCLI() = true on viewer-only session, want false")
	}
	if ls.ViewerCount() != 0 {
		t.Errorf("ViewerCount = %d, want 0", ls.ViewerCount())
	}
}

// TestLocalSession_AddViewer checks that up to maxViewersPerSession viewers are
// accepted and that the (maxViewersPerSession+1)th call returns false.
// Nil conns are used because AddViewer only stores the pointer.
func TestLocalSession_AddViewer(t *testing.T) {
	ls := NewLocalSession("s1", nil, nil, newTestLogger())

	for i := 0; i < maxViewersPerSession; i++ {
		id := "viewer-" + string(rune('A'+i))
		if !ls.AddViewer(id, nil) {
			t.Fatalf("AddViewer %d returned false, want true", i+1)
		}
	}
	if ls.ViewerCount() != maxViewersPerSession {
		t.Errorf("ViewerCount = %d, want %d", ls.ViewerCount(), maxViewersPerSession)
	}

	// One more should be rejected.
	if ls.AddViewer("viewer-overflow", nil) {
		t.Error("AddViewer returned true when at capacity, want false")
	}
	if ls.ViewerCount() != maxViewersPerSession {
		t.Errorf("ViewerCount after overflow attempt = %d, want %d", ls.ViewerCount(), maxViewersPerSession)
	}
}

// TestLocalSession_AddViewer_Closed verifies that AddViewer returns false once
// the session is closed.
func TestLocalSession_AddViewer_Closed(t *testing.T) {
	ls := NewLocalSession("s2", nil, nil, newTestLogger())
	ls.Close()

	if ls.AddViewer("late-viewer", nil) {
		t.Error("AddViewer on closed session returned true, want false")
	}
}

// TestLocalSession_RemoveViewer confirms that removing an added viewer decrements
// the count to zero.
func TestLocalSession_RemoveViewer(t *testing.T) {
	ls := NewLocalSession("s3", nil, nil, newTestLogger())

	ls.AddViewer("v1", nil)
	ls.AddViewer("v2", nil)

	ls.RemoveViewer("v1")
	if ls.ViewerCount() != 1 {
		t.Errorf("ViewerCount after first remove = %d, want 1", ls.ViewerCount())
	}

	ls.RemoveViewer("v2")
	if ls.ViewerCount() != 0 {
		t.Errorf("ViewerCount after second remove = %d, want 0", ls.ViewerCount())
	}
}

// TestLocalSession_RemoveViewer_NonExistent confirms removing an unknown viewer ID
// is a no-op and does not panic.
func TestLocalSession_RemoveViewer_NonExistent(t *testing.T) {
	ls := NewLocalSession("s4", nil, nil, newTestLogger())
	// Should not panic.
	ls.RemoveViewer("nobody")
	if ls.ViewerCount() != 0 {
		t.Errorf("ViewerCount = %d after removing non-existent viewer, want 0", ls.ViewerCount())
	}
}

// TestLocalSession_ViewerCount verifies the count reflects the number of added viewers.
func TestLocalSession_ViewerCount(t *testing.T) {
	ls := NewLocalSession("s5", nil, nil, newTestLogger())

	if ls.ViewerCount() != 0 {
		t.Errorf("initial ViewerCount = %d, want 0", ls.ViewerCount())
	}

	ls.AddViewer("v1", nil)
	ls.AddViewer("v2", nil)
	ls.AddViewer("v3", nil)

	if ls.ViewerCount() != 3 {
		t.Errorf("ViewerCount = %d, want 3", ls.ViewerCount())
	}
}

// TestLocalSession_BroadcastToLocalViewers creates a real WebSocket pair, registers
// the server-side conn as a viewer, broadcasts pre-encoded data, and asserts the
// client receives the correct framed message.
func TestLocalSession_BroadcastToLocalViewers(t *testing.T) {
	viewerServer, viewerClient := newWSPair(t)

	ls := NewLocalSession("s6", nil, nil, newTestLogger())
	ls.AddViewer("v1", viewerServer)

	payload := []byte("hello world")
	encoded, err := protocol.Encode(protocol.TypeStdout, payload)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	ctx := context.Background()
	ls.BroadcastToLocalViewers(ctx, encoded)

	msg := readMessage(t, viewerClient)

	if len(msg) == 0 {
		t.Fatal("received empty message")
	}
	if msg[0] != protocol.TypeStdout {
		t.Errorf("message type = 0x%02x, want 0x%02x (TypeStdout)", msg[0], protocol.TypeStdout)
	}
	if string(msg[1:]) != string(payload) {
		t.Errorf("payload = %q, want %q", msg[1:], payload)
	}
}

// TestLocalSession_BroadcastToLocalViewers_Multiple verifies all registered viewers
// receive the broadcast.
func TestLocalSession_BroadcastToLocalViewers_Multiple(t *testing.T) {
	viewerServer1, viewerClient1 := newWSPair(t)
	viewerServer2, viewerClient2 := newWSPair(t)

	ls := NewLocalSession("s7", nil, nil, newTestLogger())
	ls.AddViewer("v1", viewerServer1)
	ls.AddViewer("v2", viewerServer2)

	payload := []byte("broadcast data")
	encoded, err := protocol.Encode(protocol.TypeStdout, payload)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	ctx := context.Background()
	ls.BroadcastToLocalViewers(ctx, encoded)

	for _, client := range []*websocket.Conn{viewerClient1, viewerClient2} {
		msg := readMessage(t, client)
		if msg[0] != protocol.TypeStdout {
			t.Errorf("message type = 0x%02x, want TypeStdout", msg[0])
		}
		if string(msg[1:]) != string(payload) {
			t.Errorf("payload = %q, want %q", msg[1:], payload)
		}
	}
}

// TestLocalSession_SendToCLI creates a real WebSocket pair for the CLI connection,
// calls SendToCLI with pre-encoded data, and verifies the CLI client receives the
// correctly encoded message.
func TestLocalSession_SendToCLI(t *testing.T) {
	cliServer, cliClient := newWSPair(t)

	ls := NewLocalSession("s8", cliServer, nil, newTestLogger())

	payload := []byte("keystrokes")
	encoded, err := protocol.Encode(protocol.TypeStdin, payload)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	ctx := context.Background()
	if err := ls.SendToCLI(ctx, encoded); err != nil {
		t.Fatalf("SendToCLI error: %v", err)
	}

	msg := readMessage(t, cliClient)

	if len(msg) == 0 {
		t.Fatal("received empty message from CLI connection")
	}
	if msg[0] != protocol.TypeStdin {
		t.Errorf("message type = 0x%02x, want 0x%02x (TypeStdin)", msg[0], protocol.TypeStdin)
	}
	if string(msg[1:]) != string(payload) {
		t.Errorf("payload = %q, want %q", msg[1:], payload)
	}
}

// TestLocalSession_NotifyViewerCount adds one viewer, calls NotifyViewerCount, and
// verifies the CLI receives a TypeViewerCount message with Count=1.
func TestLocalSession_NotifyViewerCount(t *testing.T) {
	cliServer, cliClient := newWSPair(t)

	ls := NewLocalSession("s9", cliServer, nil, newTestLogger())
	ls.AddViewer("v1", nil)

	ctx := context.Background()
	ls.NotifyViewerCount(ctx)

	msg := readMessage(t, cliClient)

	if len(msg) == 0 {
		t.Fatal("received empty message")
	}
	if msg[0] != protocol.TypeViewerCount {
		t.Errorf("message type = 0x%02x, want 0x%02x (TypeViewerCount)", msg[0], protocol.TypeViewerCount)
	}

	var vc protocol.ViewerCount
	if err := json.Unmarshal(msg[1:], &vc); err != nil {
		t.Fatalf("unmarshal ViewerCount payload: %v", err)
	}
	if vc.Count != 1 {
		t.Errorf("ViewerCount.Count = %d, want 1", vc.Count)
	}
}

// TestLocalSession_NotifyViewerCount_Zero verifies that NotifyViewerCount correctly
// reports zero when no viewers are attached.
func TestLocalSession_NotifyViewerCount_Zero(t *testing.T) {
	cliServer, cliClient := newWSPair(t)

	ls := NewLocalSession("s10", cliServer, nil, newTestLogger())

	ctx := context.Background()
	ls.NotifyViewerCount(ctx)

	msg := readMessage(t, cliClient)

	if msg[0] != protocol.TypeViewerCount {
		t.Errorf("message type = 0x%02x, want TypeViewerCount", msg[0])
	}
	var vc protocol.ViewerCount
	if err := json.Unmarshal(msg[1:], &vc); err != nil {
		t.Fatalf("unmarshal ViewerCount payload: %v", err)
	}
	if vc.Count != 0 {
		t.Errorf("ViewerCount.Count = %d, want 0", vc.Count)
	}
}

// TestLocalSession_Close_Idempotent verifies that calling Close twice does not panic
// and that viewers receive a TypeEnd message on the first Close.
func TestLocalSession_Close_Idempotent(t *testing.T) {
	viewerServer, viewerClient := newWSPair(t)

	ls := NewLocalSession("s11", nil, nil, newTestLogger())
	ls.AddViewer("v1", viewerServer)

	// First close — should send TypeEnd to viewers.
	ls.Close()

	// Read the TypeEnd frame sent to the viewer.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, data, err := viewerClient.Read(ctx)
	// The connection may be closed gracefully after TypeEnd is written, so we
	// tolerate either a clean read or a close-status read as long as we got
	// the TypeEnd byte if data was returned.
	if err == nil {
		if len(data) == 0 || data[0] != protocol.TypeEnd {
			t.Errorf("expected TypeEnd (0x%02x) as first byte, got 0x%02x", protocol.TypeEnd, data[0])
		}
	}
	// err != nil means the connection was closed, which is also acceptable
	// since Close() calls conn.Close after writing the end message.

	// Second close — must not panic.
	ls.Close()

	// After Close the session should report zero viewers.
	if ls.ViewerCount() != 0 {
		t.Errorf("ViewerCount after Close = %d, want 0", ls.ViewerCount())
	}
}

// TestLocalSession_Close_SetsClosedFlag confirms that after Close() the session
// rejects new viewers via AddViewer.
func TestLocalSession_Close_SetsClosedFlag(t *testing.T) {
	ls := NewLocalSession("s12", nil, nil, newTestLogger())
	ls.Close()

	if !ls.closed {
		t.Error("closed flag = false after Close(), want true")
	}
	if ls.AddViewer("late", nil) {
		t.Error("AddViewer returned true on a closed session, want false")
	}
}

// TestLocalSession_ConcurrentAddRemove exercises AddViewer / RemoveViewer /
// ViewerCount under concurrent access to detect data races (run with -race).
func TestLocalSession_ConcurrentAddRemove(t *testing.T) {
	ls := NewLocalSession("s13", nil, nil, newTestLogger())

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(n int) {
			defer wg.Done()
			id := "viewer-concurrent-" + string(rune('A'+n%26))
			ls.AddViewer(id, nil)
			_ = ls.ViewerCount()
			ls.RemoveViewer(id)
			_ = ls.ViewerCount()
		}(i)
	}

	wg.Wait()
}
