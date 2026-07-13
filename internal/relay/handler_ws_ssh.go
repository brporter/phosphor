package relay

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"github.com/brporter/phosphor/internal/store"
)

const (
	// maxBridgesPerMachine caps concurrent browser sessions to one machine.
	maxBridgesPerMachine = 16
	// bridgeIdleTimeout closes a session after this long with no traffic.
	bridgeIdleTimeout = 30 * time.Minute
)

// bridgeCounts tracks live bridges per machine for the concurrency cap.
type bridgeCounts struct {
	mu sync.Mutex
	n  map[string]int
}

func (b *bridgeCounts) acquire(machineID string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.n == nil {
		b.n = make(map[string]int)
	}
	if b.n[machineID] >= maxBridgesPerMachine {
		return false
	}
	b.n[machineID]++
	return true
}

func (b *bridgeCounts) release(machineID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.n[machineID] > 0 {
		b.n[machineID]--
	}
}

// HandleSSHBridge bridges a browser WebSocket to a machine's SSH tunnel. The
// browser runs a full SSH client; the relay only pipes ciphertext, so it
// never sees terminal contents. Auth happens in-protocol: the first frame is
// a JSON {token} that must resolve to a user whose tenant owns the machine.
//
// GET /ws/ssh/{machineID}
func (s *Server) HandleSSHBridge(w http.ResponseWriter, r *http.Request) {
	if s.tunnels == nil {
		http.Error(w, "ssh gateway not configured", http.StatusServiceUnavailable)
		return
	}
	machineID := r.PathValue("machineID")

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		Subprotocols: []string{"phosphor-ssh"},
	})
	if err != nil {
		s.logger.Debug("accept ssh bridge ws", "err", err)
		return
	}
	defer conn.CloseNow()

	// Auth prelude: {"token": "..."} as the first message.
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	authCtx, authCancel := context.WithTimeout(ctx, 10*time.Second)
	_, data, err := conn.Read(authCtx)
	authCancel()
	if err != nil {
		conn.Close(websocket.StatusPolicyViolation, "expected auth message")
		return
	}
	var authMsg struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(data, &authMsg); err != nil {
		conn.Close(websocket.StatusPolicyViolation, "invalid auth message")
		return
	}

	provider, sub, email, err := s.verifyToken(ctx, authMsg.Token)
	if err != nil {
		conn.Close(websocket.StatusPolicyViolation, "authentication failed")
		return
	}
	user, err := s.db.GetOrCreateUser(ctx, provider, sub, email)
	if err != nil {
		s.logger.Error("resolving user for ssh bridge", "err", err)
		conn.Close(websocket.StatusInternalError, "internal error")
		return
	}

	id, err := uuid.Parse(machineID)
	if err != nil {
		conn.Close(websocket.StatusPolicyViolation, "machine not found")
		return
	}
	machine, err := s.db.GetMachine(ctx, id)
	if errors.Is(err, store.ErrNotFound) || (err == nil && machine.TenantID != user.TenantID) {
		conn.Close(websocket.StatusPolicyViolation, "machine not found")
		return
	}
	if err != nil {
		s.logger.Error("loading machine for ssh bridge", "err", err)
		conn.Close(websocket.StatusInternalError, "internal error")
		return
	}

	if !s.bridges.acquire(machineID) {
		conn.Close(websocket.StatusTryAgainLater, "too many concurrent sessions")
		return
	}
	defer s.bridges.release(machineID)

	tunnelConn, err := s.tunnels.Dial(machineID)
	if err != nil {
		s.logger.Info("ssh bridge dial failed", "machine", machineID, "err", err)
		conn.Close(websocket.StatusTryAgainLater, "machine offline")
		return
	}
	defer tunnelConn.Close()

	// Confirm readiness so the client can start its SSH handshake.
	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"ok":true}`)); err != nil {
		return
	}

	s.logger.Info("ssh bridge open", "machine", machineID, "user", user.ID)
	wsConn := websocket.NetConn(ctx, conn, websocket.MessageBinary)
	pipe(ctx, wsConn, tunnelConn, cancel)
	s.logger.Info("ssh bridge closed", "machine", machineID, "user", user.ID)
	conn.Close(websocket.StatusNormalClosure, "session ended")
}

// pipe copies bytes both ways until either side closes or the session goes
// idle, then cancels ctx so both copies unwind.
func pipe(ctx context.Context, a, b net.Conn, cancel context.CancelFunc) {
	var active atomic.Bool
	var wg sync.WaitGroup
	wg.Add(2)
	copyOne := func(dst, src net.Conn) {
		defer wg.Done()
		defer cancel()
		buf := make([]byte, 32<<10)
		for {
			n, err := src.Read(buf)
			if n > 0 {
				active.Store(true)
				if _, werr := dst.Write(buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}
	go copyOne(a, b)
	go copyOne(b, a)

	// Idle watchdog.
	go func() {
		ticker := time.NewTicker(bridgeIdleTimeout)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !active.Swap(false) {
					cancel()
					a.Close()
					b.Close()
					return
				}
			}
		}
	}()

	<-ctx.Done()
	a.Close()
	b.Close()
	wg.Wait()
}
