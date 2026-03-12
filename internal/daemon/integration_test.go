package daemon

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/brporter/phosphor/internal/auth"
	"github.com/brporter/phosphor/internal/protocol"
	"github.com/brporter/phosphor/internal/relay"
)

// mockPTY implements PTYProcess for testing.
type mockPTY struct {
	readCh  chan []byte
	writeCh chan []byte
	closed  chan struct{}
	once    sync.Once
}

func newMockPTY() *mockPTY {
	return &mockPTY{
		readCh:  make(chan []byte, 16),
		writeCh: make(chan []byte, 16),
		closed:  make(chan struct{}),
	}
}

func (m *mockPTY) Read(p []byte) (int, error) {
	select {
	case data, ok := <-m.readCh:
		if !ok {
			return 0, io.EOF
		}
		n := copy(p, data)
		return n, nil
	case <-m.closed:
		return 0, io.EOF
	}
}

func (m *mockPTY) Write(p []byte) (int, error) {
	select {
	case m.writeCh <- append([]byte(nil), p...):
		return len(p), nil
	case <-m.closed:
		return 0, io.ErrClosedPipe
	}
}

func (m *mockPTY) Close() error {
	m.once.Do(func() { close(m.closed) })
	return nil
}

func (m *mockPTY) Wait(_ context.Context) (int, error) {
	<-m.closed
	return 0, nil
}

func (m *mockPTY) Resize(cols, rows int) error {
	return nil
}

func (m *mockPTY) Pid() int {
	return 1
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
		t.Fatal("wsRecv read:", err)
	}
	mt, payload, err := protocol.Decode(data)
	if err != nil {
		t.Fatal("wsRecv decode:", err)
	}
	return mt, payload
}

// TestIntegration_DaemonRelayViewer exercises the full daemon->relay->viewer flow:
//  1. Start a real relay server (httptest)
//  2. Create a Daemon with a test mapping and a mock SpawnFunc
//  3. Run the daemon briefly against the relay
//  4. Verify the lazy session is registered
//  5. Connect a viewer WebSocket, send Join
//  6. Verify the viewer receives TypeJoined
//  7. Verify the mock SpawnFunc was called (relay sent SpawnRequest)
func TestIntegration_DaemonRelayViewer(t *testing.T) {
	// --- Step 1: Start relay ---
	store := relay.NewMemorySessionStore()
	hub := relay.NewHub(store, nil, "test", slog.Default())
	store.SetExpiryCallback(func(ctx context.Context, id string) {
		hub.Unregister(ctx, id)
	})
	verifier := auth.NewVerifier(slog.Default())
	authSessions := relay.NewMemoryAuthSessionStore(5 * time.Minute)
	srv := relay.NewServer(hub, slog.Default(), "http://test", verifier, true, authSessions, nil, relay.NewBlocklist(""), 60*time.Second)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	defer authSessions.Stop()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")

	// --- Step 2: Create daemon with mock spawn ---
	var spawnCalled atomic.Int32
	var mockProc *mockPTY

	spawnFunc := func(shell string, localUser string) (PTYProcess, int, int, error) {
		spawnCalled.Add(1)
		mockProc = newMockPTY()
		return mockProc, 80, 24, nil
	}

	daemonCtx, daemonCancel := context.WithCancel(context.Background())
	defer daemonCancel()

	d := &Daemon{
		Config: &Config{
			Relay: wsURL,
			Mappings: []Mapping{
				{Identity: "test@example.com", LocalUser: "testuser", Shell: "/bin/bash"},
			},
		},
		Token:  "", // dev mode
		Logger: slog.Default(),
		Spawn:  spawnFunc,
	}

	// --- Step 3: Run daemon in goroutine ---
	daemonDone := make(chan struct{})
	go func() {
		defer close(daemonDone)
		d.Run(daemonCtx)
	}()

	// Wait for the daemon to connect and register its session.
	// Poll the relay's API for the session to appear.
	var sessionID string
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		d.mu.Lock()
		for _, ms := range d.sessions {
			ms.mu.Lock()
			sid := ms.sessionID
			ms.mu.Unlock()
			if sid != "" {
				sessionID = sid
			}
		}
		d.mu.Unlock()
		if sessionID != "" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if sessionID == "" {
		t.Fatal("daemon did not register a session with the relay within 5 seconds")
	}

	t.Logf("daemon registered session: %s", sessionID)

	// --- Step 4: Verify session is registered in the hub ---
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	info, ok, err := hub.Get(ctx, sessionID)
	if err != nil {
		t.Fatalf("hub.Get error: %v", err)
	}
	if !ok {
		t.Fatalf("session %q not found in hub", sessionID)
	}
	if !info.Lazy {
		t.Error("expected session Lazy=true")
	}
	if info.ProcessRunning {
		t.Error("expected session ProcessRunning=false before viewer connects")
	}
	if info.DelegateFor != "test@example.com" {
		t.Errorf("DelegateFor = %q, want test@example.com", info.DelegateFor)
	}

	// --- Step 5: Connect viewer WebSocket ---
	viewerConn, _, err := websocket.Dial(ctx, wsURL+"/ws/view/"+sessionID, &websocket.DialOptions{
		Subprotocols: []string{"phosphor"},
	})
	if err != nil {
		t.Fatalf("dial /ws/view: %v", err)
	}
	defer viewerConn.CloseNow()

	// Send Join with a token that resolves to the delegated identity.
	// In dev mode, "delegated:test@example.com" is parsed as provider=delegated, sub=test@example.com.
	// The viewer ownership check matches because viewerSub == info.DelegateFor.
	join := protocol.Join{Token: "delegated:test@example.com", SessionID: sessionID}
	if err := wsSend(ctx, viewerConn, protocol.TypeJoin, join); err != nil {
		t.Fatalf("send Join: %v", err)
	}

	// --- Step 6: Verify viewer receives TypeJoined ---
	vmt, vpayload := wsRecv(ctx, t, viewerConn)
	if vmt != protocol.TypeJoined {
		t.Fatalf("expected TypeJoined (0x%02x), got 0x%02x", protocol.TypeJoined, vmt)
	}
	var joined protocol.Joined
	if err := protocol.DecodeJSON(vpayload, &joined); err != nil {
		t.Fatalf("decode Joined: %v", err)
	}
	if joined.Mode != "pty" {
		t.Errorf("Joined.Mode = %q, want pty", joined.Mode)
	}

	// --- Step 7: Verify mock SpawnFunc was called ---
	// The relay sends SpawnRequest to the daemon CLI when a viewer joins a lazy session.
	// The daemon's handleSpawn processes it asynchronously, so poll for it.
	spawnDeadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(spawnDeadline) {
		if spawnCalled.Load() > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if spawnCalled.Load() == 0 {
		t.Error("SpawnFunc was never called — relay did not send SpawnRequest or daemon did not handle it")
	} else {
		t.Logf("SpawnFunc called %d time(s)", spawnCalled.Load())
	}

	// --- Step 8: Clean up ---
	// Close the mock PTY so handleSpawn's Wait returns
	if mockProc != nil {
		mockProc.Close()
	}

	daemonCancel()

	// Wait for daemon to shut down with a timeout
	select {
	case <-daemonDone:
	case <-time.After(5 * time.Second):
		t.Log("warning: daemon did not shut down within 5 seconds")
	}
}
