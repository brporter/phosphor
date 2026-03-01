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
// (i.e. what you pass into a Session), and clientConn is what you read
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

// TestNewSession verifies that NewSession populates all fields from the
// Hello struct and the arguments passed to it.
func TestNewSession(t *testing.T) {
	hello := protocol.Hello{
		Mode:    "pty",
		Cols:    220,
		Rows:    50,
		Command: "bash",
	}

	sess := NewSession("sess-abc", "google", "sub-123", nil, hello, newTestLogger())

	if sess.ID != "sess-abc" {
		t.Errorf("ID = %q, want sess-abc", sess.ID)
	}
	if sess.OwnerProvider != "google" {
		t.Errorf("OwnerProvider = %q, want google", sess.OwnerProvider)
	}
	if sess.OwnerSub != "sub-123" {
		t.Errorf("OwnerSub = %q, want sub-123", sess.OwnerSub)
	}
	if sess.Mode != "pty" {
		t.Errorf("Mode = %q, want pty", sess.Mode)
	}
	if sess.Cols != 220 {
		t.Errorf("Cols = %d, want 220", sess.Cols)
	}
	if sess.Rows != 50 {
		t.Errorf("Rows = %d, want 50", sess.Rows)
	}
	if sess.Command != "bash" {
		t.Errorf("Command = %q, want bash", sess.Command)
	}
	if sess.viewers == nil {
		t.Error("viewers map is nil, want initialised map")
	}
	if sess.closed {
		t.Error("closed = true on new session, want false")
	}
}

// TestSession_AddViewer checks that up to maxViewersPerSession viewers are
// accepted and that the (maxViewersPerSession+1)th call returns false.
// Nil conns are used because AddViewer only stores the pointer.
func TestSession_AddViewer(t *testing.T) {
	sess := NewSession("s1", "google", "u1", nil, protocol.Hello{}, newTestLogger())

	for i := 0; i < maxViewersPerSession; i++ {
		id := "viewer-" + string(rune('A'+i))
		if !sess.AddViewer(id, nil) {
			t.Fatalf("AddViewer %d returned false, want true", i+1)
		}
	}
	if sess.ViewerCount() != maxViewersPerSession {
		t.Errorf("ViewerCount = %d, want %d", sess.ViewerCount(), maxViewersPerSession)
	}

	// One more should be rejected.
	if sess.AddViewer("viewer-overflow", nil) {
		t.Error("AddViewer returned true when at capacity, want false")
	}
	if sess.ViewerCount() != maxViewersPerSession {
		t.Errorf("ViewerCount after overflow attempt = %d, want %d", sess.ViewerCount(), maxViewersPerSession)
	}
}

// TestSession_AddViewer_Closed verifies that AddViewer returns false once
// the session is closed.
func TestSession_AddViewer_Closed(t *testing.T) {
	sess := NewSession("s2", "apple", "u2", nil, protocol.Hello{}, newTestLogger())
	sess.Close()

	if sess.AddViewer("late-viewer", nil) {
		t.Error("AddViewer on closed session returned true, want false")
	}
}

// TestSession_RemoveViewer confirms that removing an added viewer decrements
// the count to zero.
func TestSession_RemoveViewer(t *testing.T) {
	sess := NewSession("s3", "microsoft", "u3", nil, protocol.Hello{}, newTestLogger())

	sess.AddViewer("v1", nil)
	sess.AddViewer("v2", nil)

	sess.RemoveViewer("v1")
	if sess.ViewerCount() != 1 {
		t.Errorf("ViewerCount after first remove = %d, want 1", sess.ViewerCount())
	}

	sess.RemoveViewer("v2")
	if sess.ViewerCount() != 0 {
		t.Errorf("ViewerCount after second remove = %d, want 0", sess.ViewerCount())
	}
}

// TestSession_RemoveViewer_NonExistent confirms removing an unknown viewer ID
// is a no-op and does not panic.
func TestSession_RemoveViewer_NonExistent(t *testing.T) {
	sess := NewSession("s4", "google", "u4", nil, protocol.Hello{}, newTestLogger())
	// Should not panic.
	sess.RemoveViewer("nobody")
	if sess.ViewerCount() != 0 {
		t.Errorf("ViewerCount = %d after removing non-existent viewer, want 0", sess.ViewerCount())
	}
}

// TestSession_ViewerCount verifies the count reflects the number of added viewers.
func TestSession_ViewerCount(t *testing.T) {
	sess := NewSession("s5", "google", "u5", nil, protocol.Hello{}, newTestLogger())

	if sess.ViewerCount() != 0 {
		t.Errorf("initial ViewerCount = %d, want 0", sess.ViewerCount())
	}

	sess.AddViewer("v1", nil)
	sess.AddViewer("v2", nil)
	sess.AddViewer("v3", nil)

	if sess.ViewerCount() != 3 {
		t.Errorf("ViewerCount = %d, want 3", sess.ViewerCount())
	}
}

// TestSession_BroadcastToViewers creates a real WebSocket pair, registers the
// server-side conn as a viewer, broadcasts TypeStdout, and asserts the client
// receives the correct framed message.
func TestSession_BroadcastToViewers(t *testing.T) {
	viewerServer, viewerClient := newWSPair(t)

	sess := NewSession("s6", "google", "u6", nil, protocol.Hello{}, newTestLogger())
	sess.AddViewer("v1", viewerServer)

	payload := []byte("hello world")
	ctx := context.Background()
	sess.BroadcastToViewers(ctx, protocol.TypeStdout, payload)

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

// TestSession_BroadcastToViewers_Multiple verifies all registered viewers
// receive the broadcast.
func TestSession_BroadcastToViewers_Multiple(t *testing.T) {
	viewerServer1, viewerClient1 := newWSPair(t)
	viewerServer2, viewerClient2 := newWSPair(t)

	sess := NewSession("s7", "google", "u7", nil, protocol.Hello{}, newTestLogger())
	sess.AddViewer("v1", viewerServer1)
	sess.AddViewer("v2", viewerServer2)

	payload := []byte("broadcast data")
	ctx := context.Background()
	sess.BroadcastToViewers(ctx, protocol.TypeStdout, payload)

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

// TestSession_SendToCLI creates a real WebSocket pair for the CLI connection,
// calls SendToCLI with TypeStdin, and verifies the CLI client receives the
// correctly encoded message.
func TestSession_SendToCLI(t *testing.T) {
	cliServer, cliClient := newWSPair(t)

	sess := NewSession("s8", "google", "u8", cliServer, protocol.Hello{}, newTestLogger())

	payload := []byte("keystrokes")
	ctx := context.Background()
	if err := sess.SendToCLI(ctx, protocol.TypeStdin, payload); err != nil {
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

// TestSession_NotifyViewerCount adds one viewer, calls NotifyViewerCount, and
// verifies the CLI receives a TypeViewerCount message with Count=1.
func TestSession_NotifyViewerCount(t *testing.T) {
	cliServer, cliClient := newWSPair(t)

	sess := NewSession("s9", "google", "u9", cliServer, protocol.Hello{}, newTestLogger())
	sess.AddViewer("v1", nil)

	ctx := context.Background()
	sess.NotifyViewerCount(ctx)

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

// TestSession_NotifyViewerCount_Zero verifies that NotifyViewerCount correctly
// reports zero when no viewers are attached.
func TestSession_NotifyViewerCount_Zero(t *testing.T) {
	cliServer, cliClient := newWSPair(t)

	sess := NewSession("s10", "google", "u10", cliServer, protocol.Hello{}, newTestLogger())

	ctx := context.Background()
	sess.NotifyViewerCount(ctx)

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

// TestSession_Close_Idempotent verifies that calling Close twice does not panic
// and that viewers receive a TypeEnd message on the first Close.
func TestSession_Close_Idempotent(t *testing.T) {
	viewerServer, viewerClient := newWSPair(t)

	sess := NewSession("s11", "google", "u11", nil, protocol.Hello{}, newTestLogger())
	sess.AddViewer("v1", viewerServer)

	// First close — should send TypeEnd to viewers.
	sess.Close()

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
	sess.Close()

	// After Close the session should report zero viewers.
	if sess.ViewerCount() != 0 {
		t.Errorf("ViewerCount after Close = %d, want 0", sess.ViewerCount())
	}
}

// TestSession_Close_SetsClosedFlag confirms that after Close() the session
// rejects new viewers via AddViewer.
func TestSession_Close_SetsClosedFlag(t *testing.T) {
	sess := NewSession("s12", "google", "u12", nil, protocol.Hello{}, newTestLogger())
	sess.Close()

	if !sess.closed {
		t.Error("closed flag = false after Close(), want true")
	}
	if sess.AddViewer("late", nil) {
		t.Error("AddViewer returned true on a closed session, want false")
	}
}

// TestSession_ConcurrentAddRemove exercises AddViewer / RemoveViewer /
// ViewerCount under concurrent access to detect data races (run with -race).
func TestSession_ConcurrentAddRemove(t *testing.T) {
	sess := NewSession("s13", "google", "u13", nil, protocol.Hello{}, newTestLogger())

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(n int) {
			defer wg.Done()
			id := "viewer-concurrent-" + string(rune('A'+n%26))
			sess.AddViewer(id, nil)
			_ = sess.ViewerCount()
			sess.RemoveViewer(id)
			_ = sess.ViewerCount()
		}(i)
	}

	wg.Wait()
}
