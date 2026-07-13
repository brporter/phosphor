package relay

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"golang.org/x/crypto/ssh"

	dbstore "github.com/brporter/phosphor/internal/store"
)

// stubTunnels pairs Dial with an in-memory pipe whose far end echoes bytes,
// standing in for a machine's sshd reached through a tunnel.
type stubTunnels struct {
	online map[string]bool
}

func (s *stubTunnels) Online(id string) bool { return s.online[id] }
func (s *stubTunnels) Close(id string) bool  { return false }
func (s *stubTunnels) Dial(id string) (net.Conn, error) {
	if !s.online[id] {
		return nil, net.ErrClosed
	}
	client, server := net.Pipe()
	go func() {
		defer server.Close()
		io.Copy(server, server)
	}()
	return client, nil
}

func newBridgeServer(t *testing.T, online bool) (*httptest.Server, string) {
	t.Helper()
	hub := NewHub(NewMemorySessionStore(), nil, "test", slog.Default())
	authSessions := NewMemoryAuthSessionStore(5 * time.Minute)
	t.Cleanup(authSessions.Stop)
	db := dbstore.NewFake()
	s := NewServer(hub, slog.Default(), "http://test", nil, true, authSessions, nil, db, 60*time.Second)

	user, err := db.GetOrCreateUser(context.Background(), "google", "alice", "alice@example.com")
	if err != nil {
		t.Fatal(err)
	}
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	sshPub, _ := ssh.NewPublicKey(pub)
	m, err := db.CreateMachine(context.Background(), user.TenantID, "box", "box.local", ssh.FingerprintSHA256(sshPub))
	if err != nil {
		t.Fatal(err)
	}

	hostPub, _, _ := ed25519.GenerateKey(rand.Reader)
	hk, _ := ssh.NewPublicKey(hostPub)
	s.SetSSHGate(&stubTunnels{online: map[string]bool{m.ID.String(): online}}, "relay:2222", hk)

	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts, m.ID.String()
}

func dialBridge(t *testing.T, ts *httptest.Server, machineID string) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws/ssh/" + machineID
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{Subprotocols: []string{"phosphor-ssh"}})
	if err != nil {
		t.Fatalf("dial bridge: %v", err)
	}
	return conn
}

func TestSSHBridge_AuthAndPipe(t *testing.T) {
	ts, machineID := newBridgeServer(t, true)
	conn := dialBridge(t, ts, machineID)
	defer conn.CloseNow()
	ctx := context.Background()

	// Auth prelude.
	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"token":"google:alice"}`)); err != nil {
		t.Fatal(err)
	}
	typ, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read ack: %v", err)
	}
	if typ != websocket.MessageText {
		t.Fatalf("ack type = %v", typ)
	}
	var ack struct {
		OK bool `json:"ok"`
	}
	json.Unmarshal(data, &ack)
	if !ack.OK {
		t.Fatalf("ack not ok: %s", data)
	}

	// Bytes echo through the stubbed tunnel.
	if err := conn.Write(ctx, websocket.MessageBinary, []byte("ping")); err != nil {
		t.Fatal(err)
	}
	typ, data, err = conn.Read(ctx)
	if err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if typ != websocket.MessageBinary || string(data) != "ping" {
		t.Fatalf("echo = %v %q", typ, data)
	}
}

func TestSSHBridge_RejectsBadToken(t *testing.T) {
	ts, machineID := newBridgeServer(t, true)
	conn := dialBridge(t, ts, machineID)
	defer conn.CloseNow()
	ctx := context.Background()

	// A non-dev server rejects, but this dev server accepts any identity —
	// so test tenant mismatch instead: bob cannot reach alice's machine.
	conn.Write(ctx, websocket.MessageText, []byte(`{"token":"google:bob"}`))
	_, _, err := conn.Read(ctx)
	if err == nil {
		t.Fatal("expected close for cross-tenant access")
	}
}

func TestSSHBridge_OfflineMachine(t *testing.T) {
	ts, machineID := newBridgeServer(t, false)
	conn := dialBridge(t, ts, machineID)
	defer conn.CloseNow()
	ctx := context.Background()

	conn.Write(ctx, websocket.MessageText, []byte(`{"token":"google:alice"}`))
	_, _, err := conn.Read(ctx)
	if err == nil {
		t.Fatal("expected close for offline machine")
	}
}

func TestSSHBridge_ConcurrencyCap(t *testing.T) {
	ts, machineID := newBridgeServer(t, true)
	ctx := context.Background()

	var conns []*websocket.Conn
	var mu sync.Mutex
	accepted := 0
	// Open more than the cap; excess should be rejected.
	for i := 0; i < maxBridgesPerMachine+3; i++ {
		conn := dialBridge(t, ts, machineID)
		conn.Write(ctx, websocket.MessageText, []byte(`{"token":"google:alice"}`))
		_, data, err := conn.Read(ctx)
		if err == nil && strings.Contains(string(data), `"ok":true`) {
			mu.Lock()
			accepted++
			conns = append(conns, conn)
			mu.Unlock()
		} else {
			conn.CloseNow()
		}
	}
	for _, c := range conns {
		c.CloseNow()
	}
	if accepted > maxBridgesPerMachine {
		t.Errorf("accepted %d bridges, cap is %d", accepted, maxBridgesPerMachine)
	}
	if accepted == 0 {
		t.Error("expected some bridges to be accepted")
	}
}
