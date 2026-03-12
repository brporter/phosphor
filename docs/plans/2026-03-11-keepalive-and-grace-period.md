# Keepalive & Grace Period Improvements Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix premature session destruction caused by idle WebSocket connections and short grace periods by adding server-side pings, CLI keepalive, configurable grace period, and daemon session recreation on TypeEnd.

**Architecture:** Four independent fixes: (1) Relay sends periodic pings to CLI connections in `HandleCLIWebSocket`, (2) CLI `app.go` starts a keepalive goroutine matching the daemon's pattern, (3) Grace period becomes configurable via `GRACE_PERIOD` env var (default 5 minutes), passed through Server struct, (4) Daemon treats `TypeEnd` as a reconnectable event by creating a fresh session instead of permanently shutting down.

**Tech Stack:** Go, WebSocket (`github.com/coder/websocket`), `internal/protocol`

---

## Chunk 1: Server-Side Pings & Configurable Grace Period

### Task 1: Add configurable grace period to Server

The grace period is currently hardcoded to 60 seconds in `handler_ws_cli.go:140`. We'll make it configurable via the `GRACE_PERIOD` env var (default 5 minutes) and thread it through the Server struct.

**Files:**
- Modify: `internal/relay/server.go` (add `gracePeriod` field)
- Modify: `cmd/relay/main.go` (read env var, pass to constructor)
- Modify: `internal/relay/handler_ws_cli.go:140` (use `s.gracePeriod`)
- Test: `internal/relay/server_test.go`

- [ ] **Step 1: Write the failing test**

Add a test in `internal/relay/server_test.go` that verifies the Server stores a custom grace period:

```go
func TestNewServer_GracePeriod(t *testing.T) {
	store := NewMemorySessionStore()
	hub := NewHub(store, nil, "test", slog.Default())
	store.SetExpiryCallback(func(ctx context.Context, id string) {
		hub.Unregister(ctx, id)
	})
	logger := slog.Default()
	verifier := auth.NewVerifier(slog.Default())
	authSessions := NewMemoryAuthSessionStore(5 * time.Minute)
	srv := NewServer(hub, logger, "http://test", verifier, true, authSessions, nil, NewBlocklist(""), 10*time.Minute)
	if srv.gracePeriod != 10*time.Minute {
		t.Errorf("gracePeriod = %v, want %v", srv.gracePeriod, 10*time.Minute)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/relay/ -run TestNewServer_GracePeriod -v`
Expected: Compilation error — `NewServer` doesn't accept grace period parameter yet.

- [ ] **Step 3: Add `gracePeriod` field to Server and update constructor**

In `internal/relay/server.go`, add `gracePeriod time.Duration` to the struct and update `NewServer`:

```go
type Server struct {
	hub          *Hub
	logger       *slog.Logger
	baseURL      string
	verifier     *auth.Verifier
	devMode      bool
	authSessions AuthSessionStoreI
	apiKeySecret []byte
	blocklist    *Blocklist
	gracePeriod  time.Duration
}

func NewServer(hub *Hub, logger *slog.Logger, baseURL string, verifier *auth.Verifier, devMode bool, authSessions AuthSessionStoreI, apiKeySecret []byte, blocklist *Blocklist, gracePeriod time.Duration) *Server {
	return &Server{hub: hub, logger: logger, baseURL: baseURL, verifier: verifier, devMode: devMode, authSessions: authSessions, apiKeySecret: apiKeySecret, blocklist: blocklist, gracePeriod: gracePeriod}
}
```

- [ ] **Step 4: Fix all existing callers of `NewServer`**

Every call to `NewServer` must now pass a grace period. Search for all callers and add a default value. The callers are:

- `cmd/relay/main.go:166` — pass the configured grace period (see Task 1 Step 6)
- `internal/relay/server_test.go:23` — pass `60*time.Second`
- `internal/relay/handler_ws_test.go:28` — pass `60*time.Second`
- `internal/relay/handler_auth_test.go:50` area — pass `60*time.Second`
- `internal/daemon/integration_test.go:117` — pass `60*time.Second`

For each, add `60*time.Second` as the final argument (or import time if needed).

- [ ] **Step 5: Use `s.gracePeriod` in `HandleCLIWebSocket`**

In `internal/relay/handler_ws_cli.go:140`, change:

```go
// Before:
defer s.hub.Disconnect(ctx, sessionID, 60*time.Second)

// After:
defer s.hub.Disconnect(ctx, sessionID, s.gracePeriod)
```

- [ ] **Step 6: Read `GRACE_PERIOD` env var in `cmd/relay/main.go`**

Add after the `devMode` line (~line 36):

```go
gracePeriod := 5 * time.Minute // default: 5 minutes
if gp := os.Getenv("GRACE_PERIOD"); gp != "" {
	parsed, err := time.ParseDuration(gp)
	if err != nil {
		logger.Error("invalid GRACE_PERIOD", "value", gp, "err", err)
		os.Exit(1)
	}
	gracePeriod = parsed
}
```

Update the `NewServer` call at line 166 to pass `gracePeriod`:

```go
srv := relay.NewServer(hub, logger, baseURL, verifier, devMode, authSessions, apiKeySecret, blocklist, gracePeriod)
```

Log it in the server startup message (line 180):

```go
logger.Info("relay server starting", "addr", addr, "base_url", baseURL, "dev_mode", devMode, "relay_id", relayID, "redis", os.Getenv("REDIS_URL") != "", "grace_period", gracePeriod)
```

- [ ] **Step 7: Add `GRACE_PERIOD` to `.env-template`**

Add after the `DEV_MODE` line:

```
# Grace period before destroying disconnected sessions (Go duration, e.g. 5m, 2m30s)
GRACE_PERIOD=5m
```

- [ ] **Step 8: Run all tests to verify nothing is broken**

Run: `go test ./... -count=1`
Expected: All pass.

- [ ] **Step 9: Commit**

```bash
git add internal/relay/server.go internal/relay/handler_ws_cli.go cmd/relay/main.go .env-template internal/relay/server_test.go internal/relay/handler_ws_test.go internal/relay/handler_auth_test.go internal/daemon/integration_test.go
git commit -m "feat: make grace period configurable via GRACE_PERIOD env var (default 5m)"
```

---

### Task 2: Add server-side keepalive pings in HandleCLIWebSocket

The relay currently never sends pings to CLI connections — it only responds to pings from the daemon. This means idle CLI connections can be silently dropped by NAT/firewalls without the relay knowing. We'll add a ping goroutine in the CLI WebSocket handler.

**Files:**
- Modify: `internal/relay/handler_ws_cli.go` (add ping goroutine in read loop)

- [ ] **Step 1: Add keepalive constants and goroutine to `HandleCLIWebSocket`**

In `internal/relay/handler_ws_cli.go`, add constants at the top of the file (after the imports):

```go
const (
	relayPingInterval = 30 * time.Second
	relayPingTimeout  = 15 * time.Second
)
```

Add the `"time"` import.

After line 140 (`defer s.hub.Disconnect(...)`) and before the read loop (line 142), add a keepalive goroutine:

```go
// Start server-side keepalive pings to detect dead connections
// and keep WebSocket alive through NAT/firewalls.
connCtx, connCancel := context.WithCancel(ctx)
defer connCancel()

go func() {
	ticker := time.NewTicker(relayPingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-connCtx.Done():
			return
		case <-ticker.C:
			pingData, err := protocol.Encode(protocol.TypePing, nil)
			if err != nil {
				continue
			}
			writeCtx, cancel := context.WithTimeout(connCtx, relayPingTimeout)
			err = conn.Write(writeCtx, websocket.MessageBinary, pingData)
			cancel()
			if err != nil {
				s.logger.Info("cli keepalive ping failed", "session", sessionID, "err", err)
				conn.CloseNow()
				return
			}
		}
	}
}()
```

- [ ] **Step 2: Run tests**

Run: `go test ./internal/relay/ -count=1`
Expected: All pass.

- [ ] **Step 3: Commit**

```bash
git add internal/relay/handler_ws_cli.go
git commit -m "feat: add server-side keepalive pings to CLI WebSocket connections"
```

---

## Chunk 2: CLI Keepalive & Daemon TypeEnd Handling

### Task 3: Add keepalive pings to the CLI

The regular `phosphor` CLI does not send keepalive pings (unlike the daemon). This means idle CLI connections can silently die. We'll add a keepalive goroutine to `runConnection` in `app.go`, matching the daemon's pattern.

**Files:**
- Modify: `internal/cli/app.go` (add keepalive goroutine in `runConnection`)

- [ ] **Step 1: Add keepalive constants**

In `internal/cli/app.go`, add after the `backoffSchedule` var (line 41):

```go
const (
	cliPingInterval = 30 * time.Second
	cliPingTimeout  = 15 * time.Second
)
```

- [ ] **Step 2: Add keepalive goroutine to `runConnection`**

In `runConnection`, after the `connCtx, connCancel` creation (line 279-280) and before the stdout bridge goroutine (line 293), add:

```go
// Keepalive: send periodic pings to detect dead connections
// and keep WebSocket alive through NAT/firewalls.
go func() {
	ticker := time.NewTicker(cliPingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-connCtx.Done():
			return
		case <-ticker.C:
			if err := ws.Send(connCtx, protocol.TypePing, nil); err != nil {
				a.Logger.Debug("keepalive ping failed", "err", err)
				closeConnLost()
				return
			}
		}
	}
}()
```

Note: We use `ws.Send` which internally calls `protocol.Encode` + `conn.Write`. The send uses `connCtx` which will be cancelled when the connection drops, providing a natural timeout.

- [ ] **Step 3: Run tests**

Run: `go test ./internal/cli/ -count=1`
Expected: All pass.

- [ ] **Step 4: Commit**

```bash
git add internal/cli/app.go
git commit -m "feat: add keepalive pings to CLI WebSocket connections"
```

---

### Task 4: Daemon creates fresh session after TypeEnd

When the daemon receives `TypeEnd` (session destroyed by grace period expiry), it currently treats this as a clean exit and stops the mapping. Instead, it should clear its saved session/reconnect token and create a fresh session on the next connection attempt.

**Files:**
- Modify: `internal/daemon/daemon.go` (handle TypeEnd as reconnectable in `readLoop` and `runMapping`)

- [ ] **Step 1: Write a test for TypeEnd reconnection behavior**

In `internal/daemon/daemon_test.go` (or create it if it doesn't exist), write a test that verifies `readLoop` returns a distinguishable error for TypeEnd:

First, check if `daemon_test.go` exists. If not, we'll test this behavior through the existing integration test or by modifying the readLoop return value.

The simplest approach: make `readLoop` return a sentinel error for TypeEnd so `runMapping` knows to clear session state and reconnect.

- [ ] **Step 2: Define sentinel error and update readLoop**

In `internal/daemon/daemon.go`, add a sentinel error after the imports:

```go
var errSessionEnded = fmt.Errorf("session ended by relay")
```

In `readLoop`, change the `TypeEnd` case (line 404-406) from:

```go
case protocol.TypeEnd:
	d.Logger.Info("session ended by relay", "identity", mapping.Identity)
	return nil
```

To:

```go
case protocol.TypeEnd:
	d.Logger.Info("session ended by relay, will reconnect with fresh session", "identity", mapping.Identity)
	return errSessionEnded
```

- [ ] **Step 3: Update runMapping to handle errSessionEnded**

In `runMapping`, after the `err := d.runConnection(ctx, mapping)` call (around line 213), add handling for the sentinel error. The key change: when `errSessionEnded` is returned, clear the managed session's sessionID and reconnectToken so the next connection creates a fresh session instead of trying to reconnect to the destroyed one.

Change the error handling block in `runMapping` (lines 214-250). After the `if ctx.Err() != nil` check, add:

```go
if errors.Is(err, errSessionEnded) {
	// Session was destroyed (e.g. grace period expired).
	// Clear session state so next connection creates a fresh session.
	ms := d.getSession(mapping.Identity)
	if ms != nil {
		ms.mu.Lock()
		ms.sessionID = ""
		ms.reconnectToken = ""
		ms.mu.Unlock()
	}
	d.Logger.Info("session ended, will create fresh session", "identity", mapping.Identity)
	// Reset backoff — this isn't a connection failure
	attempt = 0
	continue
}
```

Also add `"errors"` to the imports if not already present.

- [ ] **Step 4: Update runConnection to use saved session state for reconnection**

Check how `runConnection` builds the Hello message. Currently (line 294-300):

```go
hello := protocol.Hello{
	Token:       d.Token,
	Mode:        "pty",
	Command:     mapping.Shell,
	Hostname:    hostname,
	Lazy:        true,
	DelegateFor: mapping.Identity,
}
```

The daemon's Hello doesn't include SessionID/ReconnectToken for reconnection. Check if the daemon already handles reconnection via `managedSession.sessionID` and `managedSession.reconnectToken`. If so, the clearing in Step 3 is sufficient. If not, verify that `runConnection` reads these fields when building the Hello.

Look at `runConnection` around lines 290-340. If it already uses `ms.sessionID` and `ms.reconnectToken` in the Hello message, then clearing them in Step 3 ensures a fresh session. If not, this step is already correct — the daemon always creates fresh sessions.

- [ ] **Step 5: Run tests**

Run: `go test ./internal/daemon/ -count=1`
Expected: All pass.

- [ ] **Step 6: Run full test suite**

Run: `go test ./... -count=1`
Expected: All pass.

- [ ] **Step 7: Commit**

```bash
git add internal/daemon/daemon.go
git commit -m "feat: daemon reconnects with fresh session after TypeEnd instead of shutting down"
```

---

## Chunk 3: Documentation & Final Verification

### Task 5: Update documentation

**Files:**
- Modify: `docs/DEVELOPMENT.md` (mention `GRACE_PERIOD` env var if env vars are documented there)

- [ ] **Step 1: Check if DEVELOPMENT.md documents env vars**

Read `docs/DEVELOPMENT.md` and check if there's an env var reference table. If so, add `GRACE_PERIOD`. If not, skip this step — the `.env-template` update in Task 1 is sufficient documentation.

- [ ] **Step 2: Commit if changes were made**

```bash
git add docs/DEVELOPMENT.md
git commit -m "docs: document GRACE_PERIOD env var"
```

### Task 6: Final verification

- [ ] **Step 1: Run full test suite**

Run: `go test ./... -count=1`
Expected: All pass.

- [ ] **Step 2: Build both binaries**

Run: `go build -o bin/phosphor ./cmd/phosphor`
Run: `go build -o bin/relay ./cmd/relay`
Expected: Both compile successfully.

- [ ] **Step 3: Verify relay starts with custom grace period**

Run: `GRACE_PERIOD=10m go run ./cmd/relay`
Expected: Log output shows `grace_period=10m0s`.
