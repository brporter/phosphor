package relay

import (
	"context"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/brporter/phosphor/internal/protocol"
)

func TestSession_ReconnectToken_Generated(t *testing.T) {
	s1 := NewSession("s1", "google", "u1", nil, protocol.Hello{}, newTestLogger())
	s2 := NewSession("s2", "google", "u2", nil, protocol.Hello{}, newTestLogger())

	if s1.ReconnectToken == "" {
		t.Error("ReconnectToken is empty on new session")
	}
	if s2.ReconnectToken == "" {
		t.Error("ReconnectToken is empty on new session")
	}
	if s1.ReconnectToken == s2.ReconnectToken {
		t.Error("two sessions have the same ReconnectToken")
	}
}

func TestSession_RotateReconnectToken(t *testing.T) {
	sess := NewSession("s1", "google", "u1", nil, protocol.Hello{}, newTestLogger())
	old := sess.ReconnectToken

	sess.RotateReconnectToken()

	if sess.ReconnectToken == old {
		t.Error("RotateReconnectToken did not change the token")
	}
	if sess.ReconnectToken == "" {
		t.Error("RotateReconnectToken produced empty token")
	}
}

func TestSession_MarkDisconnected_ReplaceCLI(t *testing.T) {
	cliServer, viewerClient := newWSPair(t)
	sess := NewSession("s1", "google", "u1", cliServer, protocol.Hello{}, newTestLogger())

	// Add a viewer so we can verify broadcasts
	viewerServer, _ := newWSPair(t)
	sess.AddViewer("v1", viewerServer)

	// Mark disconnected
	sess.MarkDisconnected()

	if !sess.IsDisconnected() {
		t.Error("IsDisconnected() = false after MarkDisconnected()")
	}

	// Replace CLI with a new connection
	newCLIServer, _ := newWSPair(t)
	sess.ReplaceCLI(newCLIServer)

	if sess.IsDisconnected() {
		t.Error("IsDisconnected() = true after ReplaceCLI()")
	}

	_ = viewerClient // keep reference alive
}

func TestHub_Disconnect_GracePeriod(t *testing.T) {
	h := NewHub(slog.Default())
	sess := NewSession("s1", "google", "u1", nil, protocol.Hello{}, newTestLogger())
	h.Register(sess)

	// Use a short grace period for testing
	h.Disconnect("s1", 200*time.Millisecond)

	// Session should still be in hub immediately
	if _, ok := h.Get("s1"); !ok {
		t.Fatal("session removed immediately, should be in grace period")
	}
	if !sess.IsDisconnected() {
		t.Error("session not marked as disconnected")
	}

	// Wait for grace period to expire
	time.Sleep(400 * time.Millisecond)

	if _, ok := h.Get("s1"); ok {
		t.Error("session still in hub after grace period expired")
	}
}

func TestHub_Disconnect_ThenReconnect(t *testing.T) {
	h := NewHub(slog.Default())
	cliServer, _ := newWSPair(t)
	sess := NewSession("s1", "google", "u1", cliServer, protocol.Hello{}, newTestLogger())
	h.Register(sess)

	h.Disconnect("s1", 5*time.Second) // long grace so it doesn't expire during test

	if !sess.IsDisconnected() {
		t.Fatal("session not marked as disconnected")
	}

	// Reconnect with a new connection
	newCLIServer, _ := newWSPair(t)
	ok := h.Reconnect("s1", newCLIServer)
	if !ok {
		t.Fatal("Reconnect returned false")
	}

	if sess.IsDisconnected() {
		t.Error("session still marked as disconnected after Reconnect")
	}

	// Session should still be in hub
	if _, ok := h.Get("s1"); !ok {
		t.Error("session removed from hub after reconnect")
	}
}

func TestHub_Disconnect_Expired(t *testing.T) {
	h := NewHub(slog.Default())
	sess := NewSession("s1", "google", "u1", nil, protocol.Hello{}, newTestLogger())
	h.Register(sess)

	h.Disconnect("s1", 100*time.Millisecond)

	// Wait for grace period to expire
	time.Sleep(250 * time.Millisecond)

	// Session should be gone
	if _, ok := h.Get("s1"); ok {
		t.Fatal("session still in hub after grace period")
	}

	// Reconnect should fail
	newConn, _ := newWSPair(t)
	ok := h.Reconnect("s1", newConn)
	if ok {
		t.Error("Reconnect succeeded on expired session")
	}
}

// connectCLI establishes a CLI session and returns the conn, session ID, and reconnect token.
func connectCLI(ctx context.Context, t *testing.T, ts *httptest.Server, token string) (*websocket.Conn, string, string) {
	t.Helper()
	conn := dialCLI(ctx, t, ts)

	hello := protocol.Hello{Token: token, Mode: "pty", Cols: 80, Rows: 24, Command: "bash"}
	if err := wsSend(ctx, conn, protocol.TypeHello, hello); err != nil {
		t.Fatal("send Hello:", err)
	}

	mt, payload := wsRecv(ctx, t, conn)
	if mt != protocol.TypeWelcome {
		t.Fatalf("expected TypeWelcome, got 0x%02x", mt)
	}
	var welcome protocol.Welcome
	if err := protocol.DecodeJSON(payload, &welcome); err != nil {
		t.Fatal("decode Welcome:", err)
	}

	return conn, welcome.SessionID, welcome.ReconnectToken
}

func TestHandleCLIWebSocket_Reconnect_Success(t *testing.T) {
	srv, ts := newWSTestServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Step 1: establish initial CLI session
	conn1, sessionID, reconnectToken := connectCLI(ctx, t, ts, "")

	if reconnectToken == "" {
		t.Fatal("Welcome.ReconnectToken is empty")
	}

	// Session should be in hub
	sess, ok := srv.hub.Get(sessionID)
	if !ok {
		t.Fatalf("session %q not found in hub", sessionID)
	}

	// Step 2: close the first connection
	conn1.Close(websocket.StatusNormalClosure, "simulating disconnect")

	// Wait for session to be marked as disconnected
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if sess.IsDisconnected() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !sess.IsDisconnected() {
		t.Fatal("session not marked as disconnected")
	}

	// Step 3: reconnect with session_id + reconnect_token
	conn2 := dialCLI(ctx, t, ts)

	hello := protocol.Hello{
		Token:          "",
		Mode:           "pty",
		Cols:           80,
		Rows:           24,
		Command:        "bash",
		SessionID:      sessionID,
		ReconnectToken: reconnectToken,
	}
	if err := wsSend(ctx, conn2, protocol.TypeHello, hello); err != nil {
		t.Fatal("send reconnect Hello:", err)
	}

	mt, payload := wsRecv(ctx, t, conn2)
	if mt != protocol.TypeWelcome {
		// If it's an error, show it
		if mt == protocol.TypeError {
			var e protocol.Error
			protocol.DecodeJSON(payload, &e)
			t.Fatalf("got TypeError instead of Welcome: %s: %s", e.Code, e.Message)
		}
		t.Fatalf("expected TypeWelcome, got 0x%02x", mt)
	}

	var welcome protocol.Welcome
	if err := protocol.DecodeJSON(payload, &welcome); err != nil {
		t.Fatal("decode Welcome:", err)
	}

	// Session ID should be the same
	if welcome.SessionID != sessionID {
		t.Errorf("reconnect SessionID = %q, want %q", welcome.SessionID, sessionID)
	}
	// New reconnect token should be different (rotated)
	if welcome.ReconnectToken == reconnectToken {
		t.Error("reconnect token was not rotated")
	}
	if welcome.ReconnectToken == "" {
		t.Error("new reconnect token is empty")
	}

	// Session should no longer be disconnected
	if sess.IsDisconnected() {
		t.Error("session still marked as disconnected after reconnect")
	}
}

func TestHandleCLIWebSocket_Reconnect_BadToken(t *testing.T) {
	_, ts := newWSTestServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn1, sessionID, _ := connectCLI(ctx, t, ts, "")

	// Close first connection
	conn1.Close(websocket.StatusNormalClosure, "disconnect")
	time.Sleep(100 * time.Millisecond) // wait for disconnect to be processed

	// Reconnect with wrong token
	conn2 := dialCLI(ctx, t, ts)

	hello := protocol.Hello{
		Token:          "",
		Mode:           "pty",
		Cols:           80,
		Rows:           24,
		SessionID:      sessionID,
		ReconnectToken: "wrong-token",
	}
	if err := wsSend(ctx, conn2, protocol.TypeHello, hello); err != nil {
		t.Fatal("send Hello:", err)
	}

	mt, payload := wsRecv(ctx, t, conn2)
	if mt != protocol.TypeError {
		t.Fatalf("expected TypeError, got 0x%02x", mt)
	}

	var errMsg protocol.Error
	if err := protocol.DecodeJSON(payload, &errMsg); err != nil {
		t.Fatal("decode Error:", err)
	}
	if errMsg.Code != "invalid_token" {
		t.Errorf("Error.Code = %q, want invalid_token", errMsg.Code)
	}
}

func TestHandleCLIWebSocket_Reconnect_WrongOwner(t *testing.T) {
	_, ts := newWSTestServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create session as dev/anonymous (empty token)
	conn1, sessionID, reconnectToken := connectCLI(ctx, t, ts, "")

	// Close first connection
	conn1.Close(websocket.StatusNormalClosure, "disconnect")
	time.Sleep(100 * time.Millisecond)

	// Reconnect as a different user
	conn2 := dialCLI(ctx, t, ts)

	hello := protocol.Hello{
		Token:          "other:user", // different identity
		Mode:           "pty",
		Cols:           80,
		Rows:           24,
		SessionID:      sessionID,
		ReconnectToken: reconnectToken,
	}
	if err := wsSend(ctx, conn2, protocol.TypeHello, hello); err != nil {
		t.Fatal("send Hello:", err)
	}

	mt, payload := wsRecv(ctx, t, conn2)
	if mt != protocol.TypeError {
		t.Fatalf("expected TypeError, got 0x%02x", mt)
	}

	var errMsg protocol.Error
	if err := protocol.DecodeJSON(payload, &errMsg); err != nil {
		t.Fatal("decode Error:", err)
	}
	if errMsg.Code != "auth_failed" {
		t.Errorf("Error.Code = %q, want auth_failed", errMsg.Code)
	}
	if !strings.Contains(errMsg.Message, "different user") {
		t.Errorf("Error.Message = %q, want to contain 'different user'", errMsg.Message)
	}
}

func TestHandleCLIWebSocket_Welcome_ContainsReconnectToken(t *testing.T) {
	_, ts := newWSTestServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, _, token := connectCLI(ctx, t, ts, "")
	if token == "" {
		t.Error("Welcome from new connection should contain a reconnect token")
	}
}
