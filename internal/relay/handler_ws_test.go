package relay

import (
	"context"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/brporter/phosphor/internal/auth"
	"github.com/brporter/phosphor/internal/protocol"
)

// newWSTestServer creates a full relay Server backed by an httptest.Server.
// devMode is enabled so tokens like "" or "provider:sub" are accepted without
// real OIDC verification.
func newWSTestServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	hub := NewHub(slog.Default())
	verifier := auth.NewVerifier(slog.Default())
	srv := NewServer(hub, slog.Default(), "http://test", verifier, true)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	t.Cleanup(func() { srv.authSessions.Stop() })
	return srv, ts
}

// wsURL converts an httptest server URL to a WebSocket URL for the given path.
func wsURL(ts *httptest.Server, path string) string {
	return "ws" + strings.TrimPrefix(ts.URL, "http") + path
}

// wsSend encodes and sends a protocol message over a WebSocket connection.
func wsSend(ctx context.Context, conn *websocket.Conn, msgType byte, payload any) error {
	data, err := protocol.Encode(msgType, payload)
	if err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageBinary, data)
}

// wsRecv reads a single binary frame and decodes it as a protocol message.
func wsRecv(ctx context.Context, t *testing.T, conn *websocket.Conn) (byte, []byte) {
	t.Helper()
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatal("read:", err)
	}
	mt, payload, err := protocol.Decode(data)
	if err != nil {
		t.Fatal("decode:", err)
	}
	return mt, payload
}

// dialCLI dials /ws/cli and returns the connection.
func dialCLI(ctx context.Context, t *testing.T, ts *httptest.Server) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.Dial(ctx, wsURL(ts, "/ws/cli"), &websocket.DialOptions{
		Subprotocols: []string{"phosphor"},
	})
	if err != nil {
		t.Fatal("dial /ws/cli:", err)
	}
	t.Cleanup(func() { conn.CloseNow() })
	return conn
}

// --- TestHandleCLIWebSocket_FullFlow ---

// TestHandleCLIWebSocket_FullFlow verifies the happy-path CLI handshake:
// Hello → Welcome, session registered in hub.
func TestHandleCLIWebSocket_FullFlow(t *testing.T) {
	srv, ts := newWSTestServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn := dialCLI(ctx, t, ts)

	// Send Hello (empty token is valid in devMode).
	hello := protocol.Hello{Token: "", Mode: "pty", Cols: 80, Rows: 24, Command: "bash"}
	if err := wsSend(ctx, conn, protocol.TypeHello, hello); err != nil {
		t.Fatal("send Hello:", err)
	}

	// Expect Welcome.
	mt, payload := wsRecv(ctx, t, conn)
	if mt != protocol.TypeWelcome {
		t.Fatalf("expected TypeWelcome (0x%02x), got 0x%02x", protocol.TypeWelcome, mt)
	}

	var welcome protocol.Welcome
	if err := protocol.DecodeJSON(payload, &welcome); err != nil {
		t.Fatal("decode Welcome:", err)
	}
	if welcome.SessionID == "" {
		t.Error("Welcome.SessionID is empty")
	}
	if welcome.ViewURL == "" {
		t.Error("Welcome.ViewURL is empty")
	}
	if !strings.Contains(welcome.ViewURL, welcome.SessionID) {
		t.Errorf("ViewURL %q does not contain SessionID %q", welcome.ViewURL, welcome.SessionID)
	}

	// Session must be registered in the hub.
	_, ok := srv.hub.Get(welcome.SessionID)
	if !ok {
		t.Errorf("session %q not found in hub after Hello/Welcome", welcome.SessionID)
	}
}

// --- TestHandleCLIWebSocket_InvalidHello ---

// TestHandleCLIWebSocket_InvalidHello verifies that sending a non-Hello message
// first results in a TypeError with code "invalid_message".
func TestHandleCLIWebSocket_InvalidHello(t *testing.T) {
	_, ts := newWSTestServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn := dialCLI(ctx, t, ts)

	// Send TypeStdout instead of TypeHello.
	if err := wsSend(ctx, conn, protocol.TypeStdout, []byte("not a hello")); err != nil {
		t.Fatal("send Stdout:", err)
	}

	mt, payload := wsRecv(ctx, t, conn)
	if mt != protocol.TypeError {
		t.Fatalf("expected TypeError (0x%02x), got 0x%02x", protocol.TypeError, mt)
	}

	var errMsg protocol.Error
	if err := protocol.DecodeJSON(payload, &errMsg); err != nil {
		t.Fatal("decode Error:", err)
	}
	if errMsg.Code != "invalid_message" {
		t.Errorf("Error.Code = %q, want invalid_message", errMsg.Code)
	}
	if !strings.Contains(errMsg.Message, "expected Hello") {
		t.Errorf("Error.Message = %q, want to contain 'expected Hello'", errMsg.Message)
	}
}

// --- TestHandleCLIWebSocket_InvalidPayload ---

// TestHandleCLIWebSocket_InvalidPayload verifies that a TypeHello message with
// malformed JSON payload results in a TypeError with code "invalid Hello payload".
func TestHandleCLIWebSocket_InvalidPayload(t *testing.T) {
	_, ts := newWSTestServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn := dialCLI(ctx, t, ts)

	// Manually craft a TypeHello frame with invalid JSON.
	raw := append([]byte{protocol.TypeHello}, []byte("{not valid json")...)
	if err := conn.Write(ctx, websocket.MessageBinary, raw); err != nil {
		t.Fatal("write raw hello:", err)
	}

	mt, payload := wsRecv(ctx, t, conn)
	if mt != protocol.TypeError {
		t.Fatalf("expected TypeError (0x%02x), got 0x%02x", protocol.TypeError, mt)
	}

	var errMsg protocol.Error
	if err := protocol.DecodeJSON(payload, &errMsg); err != nil {
		t.Fatal("decode Error:", err)
	}
	if errMsg.Code != "invalid_payload" {
		t.Errorf("Error.Code = %q, want invalid_payload", errMsg.Code)
	}
	if !strings.Contains(errMsg.Message, "invalid Hello payload") {
		t.Errorf("Error.Message = %q, want to contain 'invalid Hello payload'", errMsg.Message)
	}
}

// --- TestHandleViewerWebSocket_FullFlow ---

// TestHandleViewerWebSocket_FullFlow exercises the full CLI+viewer handshake and
// verifies that stdout written by the CLI is forwarded to the viewer.
func TestHandleViewerWebSocket_FullFlow(t *testing.T) {
	_, ts := newWSTestServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Step 1: create a CLI session.
	cliConn := dialCLI(ctx, t, ts)

	hello := protocol.Hello{Token: "", Mode: "pty", Cols: 80, Rows: 24, Command: "bash"}
	if err := wsSend(ctx, cliConn, protocol.TypeHello, hello); err != nil {
		t.Fatal("send Hello:", err)
	}

	mt, payload := wsRecv(ctx, t, cliConn)
	if mt != protocol.TypeWelcome {
		t.Fatalf("expected TypeWelcome, got 0x%02x", mt)
	}
	var welcome protocol.Welcome
	if err := protocol.DecodeJSON(payload, &welcome); err != nil {
		t.Fatal("decode Welcome:", err)
	}
	sessionID := welcome.SessionID

	// Step 2: dial the viewer WebSocket.
	viewerConn, _, err := websocket.Dial(ctx, wsURL(ts, "/ws/view/"+sessionID), &websocket.DialOptions{
		Subprotocols: []string{"phosphor"},
	})
	if err != nil {
		t.Fatal("dial /ws/view:", err)
	}
	defer viewerConn.CloseNow()

	// Send Join with the same dev identity (empty token → dev/anonymous).
	join := protocol.Join{Token: "", SessionID: sessionID}
	if err := wsSend(ctx, viewerConn, protocol.TypeJoin, join); err != nil {
		t.Fatal("send Join:", err)
	}

	// Expect Joined.
	vmt, vpayload := wsRecv(ctx, t, viewerConn)
	if vmt != protocol.TypeJoined {
		t.Fatalf("expected TypeJoined (0x%02x), got 0x%02x", protocol.TypeJoined, vmt)
	}
	var joined protocol.Joined
	if err := protocol.DecodeJSON(vpayload, &joined); err != nil {
		t.Fatal("decode Joined:", err)
	}
	if joined.Mode != "pty" {
		t.Errorf("Joined.Mode = %q, want pty", joined.Mode)
	}
	if joined.Cols != 80 {
		t.Errorf("Joined.Cols = %d, want 80", joined.Cols)
	}
	if joined.Rows != 24 {
		t.Errorf("Joined.Rows = %d, want 24", joined.Rows)
	}

	// The CLI receives a ViewerCount notification — drain it so the read loop
	// is unblocked before we send Stdout.
	cliMT, _ := wsRecv(ctx, t, cliConn)
	if cliMT != protocol.TypeViewerCount {
		t.Errorf("CLI expected TypeViewerCount (0x%02x), got 0x%02x", protocol.TypeViewerCount, cliMT)
	}

	// Step 3: CLI sends TypeStdout — viewer should receive it.
	stdoutData := []byte("hello from terminal")
	if err := wsSend(ctx, cliConn, protocol.TypeStdout, stdoutData); err != nil {
		t.Fatal("send Stdout:", err)
	}

	vmt2, vp2 := wsRecv(ctx, t, viewerConn)
	if vmt2 != protocol.TypeStdout {
		t.Fatalf("viewer expected TypeStdout (0x%02x), got 0x%02x", protocol.TypeStdout, vmt2)
	}
	if string(vp2) != string(stdoutData) {
		t.Errorf("viewer received %q, want %q", vp2, stdoutData)
	}
}

// --- TestHandleViewerWebSocket_SessionNotFound ---

// TestHandleViewerWebSocket_SessionNotFound verifies that joining a non-existent
// session returns a TypeError with code "session_not_found".
func TestHandleViewerWebSocket_SessionNotFound(t *testing.T) {
	_, ts := newWSTestServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL(ts, "/ws/view/nonexistent"), &websocket.DialOptions{
		Subprotocols: []string{"phosphor"},
	})
	if err != nil {
		t.Fatal("dial /ws/view:", err)
	}
	defer conn.CloseNow()

	join := protocol.Join{Token: "", SessionID: "nonexistent"}
	if err := wsSend(ctx, conn, protocol.TypeJoin, join); err != nil {
		t.Fatal("send Join:", err)
	}

	mt, payload := wsRecv(ctx, t, conn)
	if mt != protocol.TypeError {
		t.Fatalf("expected TypeError (0x%02x), got 0x%02x", protocol.TypeError, mt)
	}

	var errMsg protocol.Error
	if err := protocol.DecodeJSON(payload, &errMsg); err != nil {
		t.Fatal("decode Error:", err)
	}
	if errMsg.Code != "session_not_found" {
		t.Errorf("Error.Code = %q, want session_not_found", errMsg.Code)
	}
}

// --- TestHandleViewerWebSocket_NotOwner ---

// TestHandleViewerWebSocket_NotOwner verifies that a viewer with a different
// identity than the session owner receives a "forbidden" TypeError.
func TestHandleViewerWebSocket_NotOwner(t *testing.T) {
	_, ts := newWSTestServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Create CLI session as dev/anonymous (empty token in devMode).
	cliConn := dialCLI(ctx, t, ts)
	hello := protocol.Hello{Token: "", Mode: "pty", Cols: 80, Rows: 24}
	if err := wsSend(ctx, cliConn, protocol.TypeHello, hello); err != nil {
		t.Fatal("send Hello:", err)
	}

	mt, payload := wsRecv(ctx, t, cliConn)
	if mt != protocol.TypeWelcome {
		t.Fatalf("expected TypeWelcome, got 0x%02x", mt)
	}
	var welcome protocol.Welcome
	if err := protocol.DecodeJSON(payload, &welcome); err != nil {
		t.Fatal("decode Welcome:", err)
	}

	// Dial viewer with a different identity: "other:user".
	viewerConn, _, err := websocket.Dial(ctx, wsURL(ts, "/ws/view/"+welcome.SessionID), &websocket.DialOptions{
		Subprotocols: []string{"phosphor"},
	})
	if err != nil {
		t.Fatal("dial /ws/view:", err)
	}
	defer viewerConn.CloseNow()

	join := protocol.Join{Token: "other:user", SessionID: welcome.SessionID}
	if err := wsSend(ctx, viewerConn, protocol.TypeJoin, join); err != nil {
		t.Fatal("send Join:", err)
	}

	vmt, vpayload := wsRecv(ctx, t, viewerConn)
	if vmt != protocol.TypeError {
		t.Fatalf("expected TypeError (0x%02x), got 0x%02x", protocol.TypeError, vmt)
	}

	var errMsg protocol.Error
	if err := protocol.DecodeJSON(vpayload, &errMsg); err != nil {
		t.Fatal("decode Error:", err)
	}
	if errMsg.Code != "forbidden" {
		t.Errorf("Error.Code = %q, want forbidden", errMsg.Code)
	}
	if !strings.Contains(errMsg.Message, "do not own") {
		t.Errorf("Error.Message = %q, want to contain 'do not own'", errMsg.Message)
	}
}

// --- TestHandleCLIWebSocket_Disconnect ---

// TestHandleCLIWebSocket_Disconnect verifies that closing the CLI WebSocket
// marks the session as disconnected (grace period) rather than immediately removing it.
func TestHandleCLIWebSocket_Disconnect(t *testing.T) {
	srv, ts := newWSTestServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Establish CLI session.
	conn, _, err := websocket.Dial(ctx, wsURL(ts, "/ws/cli"), &websocket.DialOptions{
		Subprotocols: []string{"phosphor"},
	})
	if err != nil {
		t.Fatal("dial /ws/cli:", err)
	}

	hello := protocol.Hello{Token: "", Mode: "pty", Cols: 80, Rows: 24}
	if err := wsSend(ctx, conn, protocol.TypeHello, hello); err != nil {
		conn.CloseNow()
		t.Fatal("send Hello:", err)
	}

	mt, payload := wsRecv(ctx, t, conn)
	if mt != protocol.TypeWelcome {
		conn.CloseNow()
		t.Fatalf("expected TypeWelcome, got 0x%02x", mt)
	}
	var welcome protocol.Welcome
	if err := protocol.DecodeJSON(payload, &welcome); err != nil {
		conn.CloseNow()
		t.Fatal("decode Welcome:", err)
	}
	sessionID := welcome.SessionID

	// Confirm session is in hub.
	sess, ok := srv.hub.Get(sessionID)
	if !ok {
		conn.CloseNow()
		t.Fatalf("session %q not found in hub before disconnect", sessionID)
	}

	// Close the WebSocket connection.
	conn.Close(websocket.StatusNormalClosure, "done")

	// Poll briefly for the session to be marked as disconnected (grace period).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if sess.IsDisconnected() {
			return // success: session is in grace period
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Errorf("session %q not marked as disconnected after CLI disconnect", sessionID)
}
