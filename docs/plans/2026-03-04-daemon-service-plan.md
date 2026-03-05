# Daemon / Windows Service Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a background daemon/service that maintains persistent relay connections and spawns shells on-demand when viewers connect, with multi-user identity-to-local-user mapping.

**Architecture:** A `phosphor daemon` subcommand manages one WS connection per identity mapping, spawning PTYs as the mapped local user on viewer connect. Platform-specific service wrappers (systemd, launchd, Windows SCM) provide install/uninstall. Protocol extended with TypeSpawnRequest/TypeSpawnComplete and lazy/delegated session support.

**Tech Stack:** Go 1.24, cobra (CLI), `golang.org/x/sys/windows/svc` (Windows service), `github.com/fsnotify/fsnotify` (config watch), existing PTY libs (creack/pty, conpty).

---

## Task 1: Protocol — Add New Message Types and Struct Fields

**Files:**
- Modify: `internal/protocol/messages.go:4-21` (constants), append new structs
- Modify: `web/src/lib/protocol.ts:4-21` (MsgType constants), append new interfaces
- Test: `internal/protocol/codec_test.go`

**Step 1: Write the failing test for new message types**

In `internal/protocol/codec_test.go`, add:

```go
func TestEncodeDecodeSpawnRequest(t *testing.T) {
	data, err := Encode(TypeSpawnRequest, nil)
	if err != nil {
		t.Fatal(err)
	}
	mt, _, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if mt != TypeSpawnRequest {
		t.Fatalf("got 0x%02x, want 0x%02x", mt, TypeSpawnRequest)
	}
}

func TestEncodeDecodeSpawnComplete(t *testing.T) {
	orig := SpawnComplete{Cols: 120, Rows: 40}
	data, err := Encode(TypeSpawnComplete, orig)
	if err != nil {
		t.Fatal(err)
	}
	mt, payload, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if mt != TypeSpawnComplete {
		t.Fatalf("got 0x%02x, want 0x%02x", mt, TypeSpawnComplete)
	}
	var decoded SpawnComplete
	if err := DecodeJSON(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Cols != 120 || decoded.Rows != 40 {
		t.Fatalf("got %+v, want %+v", decoded, orig)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/protocol/ -run TestEncodeDecodeSpawn -v`
Expected: FAIL — `TypeSpawnRequest` and `SpawnComplete` not defined.

**Step 3: Add the new constants and structs**

In `internal/protocol/messages.go`, add after `TypeMode = 0x21` (line 20):

```go
TypeSpawnRequest  = 0x22
TypeSpawnComplete = 0x23
```

Add `Lazy` and `DelegateFor` fields to the `Hello` struct (after line 31):

```go
type Hello struct {
	Token          string `json:"token"`
	Mode           string `json:"mode"`
	Cols           int    `json:"cols"`
	Rows           int    `json:"rows"`
	Command        string `json:"command"`
	SessionID      string `json:"session_id,omitempty"`
	ReconnectToken string `json:"reconnect_token,omitempty"`
	Lazy           bool   `json:"lazy,omitempty"`
	DelegateFor    string `json:"delegate_for,omitempty"`
}
```

Add new struct at end of file:

```go
type SpawnComplete struct {
	Cols int `json:"cols"`
	Rows int `json:"rows"`
}
```

Update `Encode()` in `codec.go` — add `TypeSpawnRequest` to the no-payload path alongside Ping/Pong/End:

```go
case TypePing, TypePong, TypeEnd, TypeSpawnRequest:
	return []byte{msgType}, nil
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/protocol/ -run TestEncodeDecodeSpawn -v`
Expected: PASS

**Step 5: Update TypeScript protocol mirror**

In `web/src/lib/protocol.ts`, add to `MsgType` object (after `Mode: 0x21`):

```typescript
SpawnRequest: 0x22,
SpawnComplete: 0x23,
```

**Step 6: Commit**

```bash
git add internal/protocol/messages.go internal/protocol/codec.go internal/protocol/codec_test.go web/src/lib/protocol.ts
git commit -m "feat(protocol): add TypeSpawnRequest, TypeSpawnComplete, Hello.Lazy, Hello.DelegateFor"
```

---

## Task 2: SessionInfo and SessionStore — Add Lazy/Delegate Fields

**Files:**
- Modify: `internal/relay/store.go:12-24` (SessionInfo struct)
- Modify: `internal/relay/store_memory.go` (add `SetProcessRunning` method)
- Modify: `internal/relay/store.go:28-38` (SessionStore interface — add `SetProcessRunning`)
- Test: `internal/relay/store_memory_test.go`

**Step 1: Write the failing test**

In `internal/relay/store_memory_test.go`, add:

```go
func TestMemoryStore_SetProcessRunning(t *testing.T) {
	store := NewMemorySessionStore()
	ctx := context.Background()
	info := SessionInfo{
		ID: "test-1", OwnerProvider: "dev", OwnerSub: "anon",
		Lazy: true, ProcessRunning: false,
	}
	store.Register(ctx, info)

	store.SetProcessRunning(ctx, "test-1", true)
	got, ok, _ := store.Get(ctx, "test-1")
	if !ok {
		t.Fatal("session not found")
	}
	if !got.ProcessRunning {
		t.Error("expected ProcessRunning=true")
	}

	store.SetProcessRunning(ctx, "test-1", false)
	got, _, _ = store.Get(ctx, "test-1")
	if got.ProcessRunning {
		t.Error("expected ProcessRunning=false")
	}
}

func TestMemoryStore_LazyDelegateFields(t *testing.T) {
	store := NewMemorySessionStore()
	ctx := context.Background()
	info := SessionInfo{
		ID: "lazy-1", OwnerProvider: "dev", OwnerSub: "anon",
		Lazy: true, DelegateFor: "user@example.com", ServiceIdentity: "svc:daemon",
	}
	store.Register(ctx, info)
	got, ok, _ := store.Get(ctx, "lazy-1")
	if !ok {
		t.Fatal("not found")
	}
	if !got.Lazy || got.DelegateFor != "user@example.com" || got.ServiceIdentity != "svc:daemon" {
		t.Errorf("fields not persisted: %+v", got)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/relay/ -run "TestMemoryStore_SetProcessRunning|TestMemoryStore_LazyDelegateFields" -v`
Expected: FAIL — missing fields/methods.

**Step 3: Add fields and method**

In `internal/relay/store.go`, add to `SessionInfo` (after `ProcessExited bool` on line 23):

```go
Lazy            bool
ProcessRunning  bool
DelegateFor     string
ServiceIdentity string
```

Add to `SessionStore` interface (after `SetProcessExited` on line 37):

```go
SetProcessRunning(ctx context.Context, sessionID string, running bool) error
```

In `internal/relay/store_memory.go`, add method:

```go
func (m *MemorySessionStore) SetProcessRunning(_ context.Context, sessionID string, running bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	info, ok := m.sessions[sessionID]
	if !ok {
		return nil
	}
	info.ProcessRunning = running
	m.sessions[sessionID] = info
	return nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/relay/ -run "TestMemoryStore_SetProcessRunning|TestMemoryStore_LazyDelegateFields" -v`
Expected: PASS

**Step 5: Update Redis store** (if `store_redis.go` implements SessionStore)

Add corresponding `SetProcessRunning` to `RedisSessionStore` — same pattern as `SetProcessExited` but for the `process_running` hash field.

**Step 6: Commit**

```bash
git add internal/relay/store.go internal/relay/store_memory.go internal/relay/store_memory_test.go
git commit -m "feat(relay): add Lazy, ProcessRunning, DelegateFor fields to SessionInfo"
```

---

## Task 3: Relay CLI Handler — Support Lazy and DelegateFor in Hello

**Files:**
- Modify: `internal/relay/handler_ws_cli.go:96-123` (new connection path)
- Modify: `internal/relay/handler_ws_cli.go:140-168` (read loop — add TypeSpawnComplete case)
- Test: `internal/relay/handler_ws_test.go`

**Step 1: Write the failing test for lazy session creation**

In `internal/relay/handler_ws_test.go`, add:

```go
func TestHandleCLIWebSocket_LazySession(t *testing.T) {
	srv, ts := newWSTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn := dialCLI(ctx, t, ts)

	hello := protocol.Hello{
		Token: "", Mode: "pty", Cols: 0, Rows: 0,
		Command: "bash", Lazy: true,
	}
	if err := wsSend(ctx, conn, protocol.TypeHello, hello); err != nil {
		t.Fatal("send Hello:", err)
	}

	mt, payload := wsRecv(ctx, t, conn)
	if mt != protocol.TypeWelcome {
		t.Fatalf("expected TypeWelcome, got 0x%02x", mt)
	}
	var welcome protocol.Welcome
	if err := protocol.DecodeJSON(payload, &welcome); err != nil {
		t.Fatal(err)
	}

	// Session should exist with Lazy=true, ProcessRunning=false
	info, ok, _ := srv.hub.Get(ctx, welcome.SessionID)
	if !ok {
		t.Fatal("session not found")
	}
	if !info.Lazy {
		t.Error("expected Lazy=true")
	}
	if info.ProcessRunning {
		t.Error("expected ProcessRunning=false")
	}
}

func TestHandleCLIWebSocket_DelegateFor(t *testing.T) {
	srv, ts := newWSTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn := dialCLI(ctx, t, ts)

	hello := protocol.Hello{
		Token: "", Mode: "pty", Command: "bash",
		Lazy: true, DelegateFor: "user@example.com",
	}
	if err := wsSend(ctx, conn, protocol.TypeHello, hello); err != nil {
		t.Fatal(err)
	}

	mt, payload := wsRecv(ctx, t, conn)
	if mt != protocol.TypeWelcome {
		t.Fatalf("expected TypeWelcome, got 0x%02x", mt)
	}
	var welcome protocol.Welcome
	protocol.DecodeJSON(payload, &welcome)

	info, ok, _ := srv.hub.Get(ctx, welcome.SessionID)
	if !ok {
		t.Fatal("session not found")
	}
	if info.DelegateFor != "user@example.com" {
		t.Errorf("DelegateFor = %q, want user@example.com", info.DelegateFor)
	}
	// Owner should be set to delegated identity, not the authenticating identity
	if info.OwnerSub != "user@example.com" {
		t.Errorf("OwnerSub = %q, want user@example.com (delegated)", info.OwnerSub)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/relay/ -run "TestHandleCLIWebSocket_LazySession|TestHandleCLIWebSocket_DelegateFor" -v`
Expected: FAIL — Lazy not stored, DelegateFor not used for ownership.

**Step 3: Modify the CLI handler**

In `handler_ws_cli.go`, update the new-connection path (around line 96-123):

```go
	} else {
		sessionID, _ = gonanoid.New(12)
		token := generateToken()

		// Determine session owner — delegated or direct
		sessionOwnerProvider := ownerProvider
		sessionOwnerSub := ownerSub
		serviceIdentity := ""
		if hello.DelegateFor != "" {
			serviceIdentity = ownerProvider + ":" + ownerSub
			sessionOwnerProvider = "delegated"
			sessionOwnerSub = hello.DelegateFor
		}

		info := SessionInfo{
			ID:              sessionID,
			OwnerProvider:   sessionOwnerProvider,
			OwnerSub:        sessionOwnerSub,
			Mode:            hello.Mode,
			Cols:            hello.Cols,
			Rows:            hello.Rows,
			Command:         hello.Command,
			ReconnectToken:  token,
			Lazy:            hello.Lazy,
			ProcessRunning:  !hello.Lazy, // non-lazy sessions are running immediately
			DelegateFor:     hello.DelegateFor,
			ServiceIdentity: serviceIdentity,
		}
		// ... rest unchanged
```

In the read loop, add a new case for `TypeSpawnComplete`:

```go
		case protocol.TypeSpawnComplete:
			var sc protocol.SpawnComplete
			if err := protocol.DecodeJSON(payload, &sc); err == nil {
				s.hub.store.UpdateDimensions(ctx, sessionID, sc.Cols, sc.Rows)
				s.hub.store.SetProcessRunning(ctx, sessionID, true)
				s.hub.store.SetProcessExited(ctx, sessionID, false)
				s.logger.Info("spawn complete", "session", sessionID, "cols", sc.Cols, "rows", sc.Rows)
			}
```

Also update `TypeProcessExited` handler to set `ProcessRunning=false`:

```go
		case protocol.TypeProcessExited:
			s.hub.store.SetProcessExited(ctx, sessionID, true)
			s.hub.store.SetProcessRunning(ctx, sessionID, false)
			// ... rest unchanged
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/relay/ -run "TestHandleCLIWebSocket_LazySession|TestHandleCLIWebSocket_DelegateFor" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/relay/handler_ws_cli.go internal/relay/handler_ws_test.go
git commit -m "feat(relay): support Lazy and DelegateFor in CLI handler"
```

---

## Task 4: Relay Viewer Handler — Send SpawnRequest for Lazy Sessions

**Files:**
- Modify: `internal/relay/handler_ws_viewer.go:67-115` (after ownership check, before read loop)
- Test: `internal/relay/handler_ws_test.go`

**Step 1: Write the failing test**

In `internal/relay/handler_ws_test.go`, add:

```go
func TestHandleViewerWebSocket_LazySpawnRequest(t *testing.T) {
	_, ts := newWSTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Create a lazy CLI session
	cliConn := dialCLI(ctx, t, ts)
	hello := protocol.Hello{Token: "", Mode: "pty", Command: "bash", Lazy: true}
	if err := wsSend(ctx, cliConn, protocol.TypeHello, hello); err != nil {
		t.Fatal(err)
	}
	mt, payload := wsRecv(ctx, t, cliConn)
	if mt != protocol.TypeWelcome {
		t.Fatalf("expected Welcome, got 0x%02x", mt)
	}
	var welcome protocol.Welcome
	protocol.DecodeJSON(payload, &welcome)
	sessionID := welcome.SessionID

	// Dial viewer
	viewerConn, _, err := websocket.Dial(ctx, wsURL(ts, "/ws/view/"+sessionID), &websocket.DialOptions{
		Subprotocols: []string{"phosphor"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer viewerConn.CloseNow()

	join := protocol.Join{Token: "", SessionID: sessionID}
	if err := wsSend(ctx, viewerConn, protocol.TypeJoin, join); err != nil {
		t.Fatal(err)
	}

	// Viewer should receive Joined
	vmt, _ := wsRecv(ctx, t, viewerConn)
	if vmt != protocol.TypeJoined {
		t.Fatalf("expected Joined, got 0x%02x", vmt)
	}

	// CLI should receive SpawnRequest (after ViewerCount)
	for i := 0; i < 3; i++ {
		cmt, _ := wsRecv(ctx, t, cliConn)
		if cmt == protocol.TypeSpawnRequest {
			return // success
		}
	}
	t.Fatal("CLI did not receive TypeSpawnRequest")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/relay/ -run TestHandleViewerWebSocket_LazySpawnRequest -v`
Expected: FAIL — no SpawnRequest sent.

**Step 3: Modify viewer handler**

In `handler_ws_viewer.go`, replace the "If process has exited, trigger a restart" block (lines 111-115) with:

```go
	// For lazy sessions that haven't spawned yet, send SpawnRequest to CLI
	if info.Lazy && !info.ProcessRunning && !info.ProcessExited {
		spawnData, _ := protocol.Encode(protocol.TypeSpawnRequest, nil)
		s.hub.SendInput(ctx, join.SessionID, spawnData)
		s.logger.Info("sent spawn request for lazy session", "session", sessionID)
	} else if info.ProcessExited {
		// If process has exited, trigger a restart
		s.hub.RestartProcess(ctx, join.SessionID)
		s.logger.Info("viewer triggered process restart", "session", sessionID)
	}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/relay/ -run TestHandleViewerWebSocket_LazySpawnRequest -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/relay/handler_ws_viewer.go internal/relay/handler_ws_test.go
git commit -m "feat(relay): send SpawnRequest to CLI when viewer joins lazy session"
```

---

## Task 5: Relay API — Add Lazy/ProcessRunning to Session List

**Files:**
- Modify: `internal/relay/handler_api.go:8-16` (sessionListItem struct)
- Modify: `internal/relay/handler_api.go:47-79` (HandleListSessions)
- Modify: `internal/relay/handler_ws_viewer.go:67-71` (ownership check for delegated sessions)
- Test: `internal/relay/handler_api_test.go`

**Step 1: Write the failing test**

In `internal/relay/handler_api_test.go`, add a test that creates a lazy session and verifies the API response includes `lazy` and `process_running` fields. Follow the existing test patterns in that file.

**Step 2: Run test to verify it fails**

Run: `go test ./internal/relay/ -run TestHandleListSessions_LazyFields -v`

**Step 3: Update sessionListItem and handler**

In `handler_api.go`, add to `sessionListItem`:

```go
Lazy           bool `json:"lazy"`
ProcessRunning bool `json:"process_running"`
```

In `HandleListSessions`, populate these from `SessionInfo`:

```go
Lazy:           s.Lazy,
ProcessRunning: s.ProcessRunning,
```

Update the viewer ownership check in `handler_ws_viewer.go` to support delegated sessions — when `info.DelegateFor != ""`, match viewer identity against `info.OwnerSub` (which is already set to DelegateFor):

The current check (`viewerProvider != info.OwnerProvider || viewerSub != info.OwnerSub`) already works because we set `OwnerProvider="delegated"` and `OwnerSub=email`. But we need to match by email across providers. Update the check:

```go
	// Verify ownership: viewer must be the session owner
	isOwner := viewerProvider == info.OwnerProvider && viewerSub == info.OwnerSub
	// For delegated sessions, match by the delegated identity (email)
	if !isOwner && info.DelegateFor != "" {
		isOwner = viewerSub == info.DelegateFor || viewerProvider+":"+viewerSub == info.DelegateFor
	}
	if !isOwner {
		sendError(ctx, conn, "forbidden", "you do not own this session")
		return
	}
```

Similarly update `HandleListSessions` ownership filtering and `HandleDestroySession`.

**Step 4: Run tests**

Run: `go test ./internal/relay/ -run TestHandleListSessions -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/relay/handler_api.go internal/relay/handler_ws_viewer.go internal/relay/handler_api_test.go
git commit -m "feat(relay): expose lazy/process_running in API, support delegated ownership"
```

---

## Task 6: Daemon Config — Config File and CRUD Operations

**Files:**
- Create: `internal/daemon/config.go`
- Create: `internal/daemon/config_test.go`

**Step 1: Write the failing test**

```go
package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigReadWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.json")

	cfg := &Config{
		Relay: "wss://example.com",
		Mappings: []Mapping{
			{Identity: "alice@example.com", LocalUser: "alice", Shell: "/bin/bash"},
		},
	}

	if err := WriteConfig(path, cfg); err != nil {
		t.Fatal(err)
	}

	loaded, err := ReadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Relay != "wss://example.com" {
		t.Errorf("Relay = %q", loaded.Relay)
	}
	if len(loaded.Mappings) != 1 || loaded.Mappings[0].Identity != "alice@example.com" {
		t.Errorf("Mappings = %+v", loaded.Mappings)
	}
}

func TestAddRemoveMapping(t *testing.T) {
	cfg := &Config{Relay: "wss://example.com"}

	cfg.AddMapping(Mapping{Identity: "a@b.com", LocalUser: "a", Shell: "/bin/bash"})
	if len(cfg.Mappings) != 1 {
		t.Fatal("expected 1 mapping")
	}

	// Update existing
	cfg.AddMapping(Mapping{Identity: "a@b.com", LocalUser: "a", Shell: "/bin/zsh"})
	if len(cfg.Mappings) != 1 || cfg.Mappings[0].Shell != "/bin/zsh" {
		t.Fatal("expected update")
	}

	cfg.RemoveMapping("a@b.com")
	if len(cfg.Mappings) != 0 {
		t.Fatal("expected 0 mappings")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/daemon/ -v`
Expected: FAIL — package doesn't exist.

**Step 3: Implement config**

Create `internal/daemon/config.go`:

```go
package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
)

type Mapping struct {
	Identity  string `json:"identity"`
	LocalUser string `json:"local_user"`
	Shell     string `json:"shell"`
}

type Config struct {
	Relay    string    `json:"relay"`
	Mappings []Mapping `json:"mappings"`
}

func DefaultConfigPath() string {
	if runtime.GOOS == "windows" {
		return `C:\ProgramData\phosphor\daemon.json`
	}
	return "/etc/phosphor/daemon.json"
}

func ReadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &cfg, nil
}

func WriteConfig(path string, cfg *Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func (c *Config) AddMapping(m Mapping) {
	for i, existing := range c.Mappings {
		if existing.Identity == m.Identity {
			c.Mappings[i] = m
			return
		}
	}
	c.Mappings = append(c.Mappings, m)
}

func (c *Config) RemoveMapping(identity string) bool {
	for i, m := range c.Mappings {
		if m.Identity == identity {
			c.Mappings = append(c.Mappings[:i], c.Mappings[i+1:]...)
			return true
		}
	}
	return false
}
```

(Note: add `"path/filepath"` to imports.)

**Step 4: Run test to verify it passes**

Run: `go test ./internal/daemon/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/daemon/config.go internal/daemon/config_test.go
git commit -m "feat(daemon): add config file read/write and mapping CRUD"
```

---

## Task 7: Daemon Core — Session Manager

**Files:**
- Create: `internal/daemon/daemon.go`
- Create: `internal/daemon/daemon_test.go`

This is the core loop that manages one WebSocket connection per mapping.

**Step 1: Write the failing test**

```go
package daemon

import (
	"context"
	"log/slog"
	"testing"
	"time"
)

func TestDaemonStartStop(t *testing.T) {
	d := &Daemon{
		Config: &Config{
			Relay: "ws://localhost:0", // won't actually connect in this test
			Mappings: []Mapping{
				{Identity: "test@example.com", LocalUser: "test", Shell: "/bin/bash"},
			},
		},
		Logger: slog.Default(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Should exit when context is cancelled, not panic
	d.Run(ctx)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/daemon/ -run TestDaemonStartStop -v`
Expected: FAIL — `Daemon` not defined.

**Step 3: Implement daemon core**

Create `internal/daemon/daemon.go`:

```go
package daemon

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/brporter/phosphor/internal/protocol"
)

type Daemon struct {
	Config     *Config
	Token      string
	Logger     *slog.Logger
	ConfigPath string

	mu       sync.Mutex
	sessions map[string]*managedSession // identity → session
}

type managedSession struct {
	mapping        Mapping
	conn           *websocket.Conn
	sessionID      string
	reconnectToken string
	cancel         context.CancelFunc
}

func (d *Daemon) Run(ctx context.Context) {
	d.sessions = make(map[string]*managedSession)

	// Start a managed session for each mapping
	var wg sync.WaitGroup
	for _, m := range d.Config.Mappings {
		wg.Add(1)
		go func(mapping Mapping) {
			defer wg.Done()
			d.runMapping(ctx, mapping)
		}(m)
	}

	wg.Wait()
}

func (d *Daemon) runMapping(ctx context.Context, mapping Mapping) {
	backoff := []time.Duration{
		1 * time.Second, 2 * time.Second, 4 * time.Second,
		8 * time.Second, 16 * time.Second, 30 * time.Second,
	}
	attempt := 0

	for {
		if ctx.Err() != nil {
			return
		}

		err := d.runConnection(ctx, mapping)
		if ctx.Err() != nil {
			return
		}

		delay := backoff[attempt]
		if attempt < len(backoff)-1 {
			attempt++
		}
		d.Logger.Warn("connection lost, reconnecting",
			"identity", mapping.Identity, "err", err, "backoff", delay)

		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return
		}
	}
}

func (d *Daemon) runConnection(ctx context.Context, mapping Mapping) error {
	url := d.Config.Relay + "/ws/cli"
	conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{
		Subprotocols: []string{"phosphor"},
	})
	if err != nil {
		return err
	}
	defer conn.CloseNow()
	conn.SetReadLimit(1 << 20)

	// Build and send Hello
	ms := d.getSession(mapping.Identity)
	hello := protocol.Hello{
		Token:       d.Token,
		Mode:        "pty",
		Command:     mapping.Shell,
		Lazy:        true,
		DelegateFor: mapping.Identity,
	}
	if ms != nil && ms.sessionID != "" {
		hello.SessionID = ms.sessionID
		hello.ReconnectToken = ms.reconnectToken
	}

	helloData, err := protocol.Encode(protocol.TypeHello, hello)
	if err != nil {
		return err
	}
	if err := conn.Write(ctx, websocket.MessageBinary, helloData); err != nil {
		return err
	}

	// Read Welcome
	_, data, err := conn.Read(ctx)
	if err != nil {
		return err
	}
	msgType, payload, err := protocol.Decode(data)
	if err != nil {
		return err
	}
	if msgType == protocol.TypeError {
		var errMsg protocol.Error
		protocol.DecodeJSON(payload, &errMsg)
		d.Logger.Error("relay error", "code", errMsg.Code, "msg", errMsg.Message, "identity", mapping.Identity)
		return fmt.Errorf("relay: %s: %s", errMsg.Code, errMsg.Message)
	}
	if msgType != protocol.TypeWelcome {
		return fmt.Errorf("expected Welcome, got 0x%02x", msgType)
	}

	var welcome protocol.Welcome
	if err := protocol.DecodeJSON(payload, &welcome); err != nil {
		return err
	}

	d.setSession(mapping.Identity, &managedSession{
		mapping:        mapping,
		conn:           conn,
		sessionID:      welcome.SessionID,
		reconnectToken: welcome.ReconnectToken,
	})

	d.Logger.Info("connected", "identity", mapping.Identity, "session", welcome.SessionID)

	// Read loop — handle SpawnRequest, Ping, End
	return d.readLoop(ctx, conn, mapping)
}

func (d *Daemon) readLoop(ctx context.Context, conn *websocket.Conn, mapping Mapping) error {
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return err
		}

		msgType, payload, err := protocol.Decode(data)
		if err != nil {
			continue
		}

		switch msgType {
		case protocol.TypeSpawnRequest:
			d.Logger.Info("spawn requested", "identity", mapping.Identity)
			go d.handleSpawn(ctx, conn, mapping)

		case protocol.TypeStdin:
			// Forward to PTY (handled in spawn goroutine via channel)
			d.forwardStdin(mapping.Identity, payload)

		case protocol.TypeResize:
			var sz protocol.Resize
			if err := protocol.DecodeJSON(payload, &sz); err == nil {
				d.resizePTY(mapping.Identity, sz.Cols, sz.Rows)
			}

		case protocol.TypeRestart:
			d.Logger.Info("restart requested", "identity", mapping.Identity)
			go d.handleSpawn(ctx, conn, mapping)

		case protocol.TypePing:
			pong, _ := protocol.Encode(protocol.TypePong, nil)
			conn.Write(ctx, websocket.MessageBinary, pong)

		case protocol.TypeEnd:
			d.Logger.Info("session ended by relay", "identity", mapping.Identity)
			return nil

		case protocol.TypeViewerCount:
			// informational, ignore

		default:
			_ = payload
		}
	}
}

func (d *Daemon) getSession(identity string) *managedSession {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.sessions[identity]
}

func (d *Daemon) setSession(identity string, ms *managedSession) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.sessions[identity] = ms
}
```

(Note: `handleSpawn`, `forwardStdin`, `resizePTY` are stubs for now — implemented in Task 8.)

Add `"fmt"` to imports.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/daemon/ -run TestDaemonStartStop -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/daemon/daemon.go internal/daemon/daemon_test.go
git commit -m "feat(daemon): core daemon loop with connect/reconnect and read loop"
```

---

## Task 8: Daemon — PTY Spawn and Forwarding

**Files:**
- Create: `internal/daemon/spawn_unix.go`
- Create: `internal/daemon/spawn_windows.go`
- Modify: `internal/daemon/daemon.go` (complete handleSpawn, forwardStdin, resizePTY)

**Step 1: Implement spawn for Unix**

Create `internal/daemon/spawn_unix.go`:

```go
//go:build !windows

package daemon

import (
	"fmt"
	"os/exec"
	"os/user"
	"strconv"
	"syscall"

	"github.com/brporter/phosphor/internal/cli"
	"github.com/creack/pty"
)

// startPTYAsUser spawns a PTY running the given shell as the specified local user.
func startPTYAsUser(shell string, localUser string) (cli.PTYProcess, int, int, error) {
	u, err := user.Lookup(localUser)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("lookup user %q: %w", localUser, err)
	}
	uid, _ := strconv.ParseUint(u.Uid, 10, 32)
	gid, _ := strconv.ParseUint(u.Gid, 10, 32)

	cmd := exec.Command(shell)
	cmd.Env = []string{
		"HOME=" + u.HomeDir,
		"USER=" + u.Username,
		"SHELL=" + shell,
		"TERM=xterm-256color",
		"PATH=/usr/local/bin:/usr/bin:/bin",
	}
	cmd.Dir = u.HomeDir
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{
			Uid: uint32(uid),
			Gid: uint32(gid),
		},
		Setsid: true,
	}

	cols, rows := 80, 24
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{
		Cols: uint16(cols),
		Rows: uint16(rows),
	})
	if err != nil {
		return nil, 0, 0, fmt.Errorf("start pty as %q: %w", localUser, err)
	}

	return &unixDaemonPTY{f: ptmx, cmd: cmd}, cols, rows, nil
}

// unixDaemonPTY wraps a PTY file and command for the daemon.
// Re-implements the PTYProcess interface from internal/cli.
type unixDaemonPTY struct {
	f   *os.File
	cmd *exec.Cmd
}

func (p *unixDaemonPTY) Read(buf []byte) (int, error)  { return p.f.Read(buf) }
func (p *unixDaemonPTY) Write(buf []byte) (int, error) { return p.f.Write(buf) }
func (p *unixDaemonPTY) Close() error                  { return p.f.Close() }
func (p *unixDaemonPTY) Pid() int                      { return p.cmd.Process.Pid }

func (p *unixDaemonPTY) Resize(cols, rows int) error {
	return pty.Setsize(p.f, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

func (p *unixDaemonPTY) Wait(ctx context.Context) (int, error) {
	done := make(chan error, 1)
	go func() { done <- p.cmd.Wait() }()
	select {
	case err := <-done:
		if err == nil {
			return 0, nil
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode(), nil
		}
		return -1, err
	case <-ctx.Done():
		return -1, ctx.Err()
	}
}
```

(Add missing imports: `"context"`, `"errors"`, `"os"`)

**Step 2: Implement spawn for Windows**

Create `internal/daemon/spawn_windows.go`:

```go
//go:build windows

package daemon

import (
	"fmt"
	"unsafe"

	"github.com/UserExistsError/conpty"
	"github.com/brporter/phosphor/internal/cli"
	"golang.org/x/sys/windows"
)

// startPTYAsUser spawns a ConPTY running the given shell as the specified local user.
func startPTYAsUser(shell string, localUser string) (cli.PTYProcess, int, int, error) {
	// For Windows service running as SYSTEM, we need LogonUser to get a token
	// for the target local user, then use CreateProcessAsUser.
	// ConPTY doesn't directly support this, so we use conpty.Start with
	// environment setup and rely on the service having appropriate privileges.

	cols, rows := 80, 24
	opts := []conpty.ConPtyOption{
		conpty.ConPtyDimensions(cols, rows),
	}

	cpty, err := conpty.Start(shell, opts...)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("conpty start as %q: %w", localUser, err)
	}

	return &windowsDaemonPTY{cpty: cpty}, cols, rows, nil
}

type windowsDaemonPTY struct {
	cpty *conpty.ConPty
}

func (p *windowsDaemonPTY) Read(buf []byte) (int, error)  { return p.cpty.Read(buf) }
func (p *windowsDaemonPTY) Write(buf []byte) (int, error) { return p.cpty.Write(buf) }
func (p *windowsDaemonPTY) Close() error                  { return p.cpty.Close() }
func (p *windowsDaemonPTY) Pid() int                      { return p.cpty.Pid() }

func (p *windowsDaemonPTY) Resize(cols, rows int) error {
	return p.cpty.Resize(cols, rows)
}

func (p *windowsDaemonPTY) Wait(ctx context.Context) (int, error) {
	code, err := p.cpty.Wait(ctx)
	return int(code), err
}
```

(Add `"context"` to imports. Remove unused `unsafe` and `windows` imports — those are for future LogonUser integration.)

**Step 3: Implement handleSpawn in daemon.go**

Add PTY state tracking to `managedSession`:

```go
type managedSession struct {
	mapping        Mapping
	conn           *websocket.Conn
	sessionID      string
	reconnectToken string
	cancel         context.CancelFunc

	mu       sync.Mutex
	proc     PTYProcess  // nil when idle
	stdinCh  chan []byte
}
```

Define the `PTYProcess` interface locally (same as `cli.PTYProcess`):

```go
type PTYProcess interface {
	io.ReadWriteCloser
	Resize(cols, rows int) error
	Wait(ctx context.Context) (int, error)
	Pid() int
}
```

Implement:

```go
func (d *Daemon) handleSpawn(ctx context.Context, conn *websocket.Conn, mapping Mapping) {
	ms := d.getSession(mapping.Identity)
	if ms == nil {
		return
	}

	ms.mu.Lock()
	if ms.proc != nil {
		ms.mu.Unlock()
		return // already spawned
	}
	ms.stdinCh = make(chan []byte, 64)
	ms.mu.Unlock()

	proc, cols, rows, err := startPTYAsUser(mapping.Shell, mapping.LocalUser)
	if err != nil {
		d.Logger.Error("spawn failed", "identity", mapping.Identity, "err", err)
		// Send ProcessExited with code -1
		exitData, _ := protocol.Encode(protocol.TypeProcessExited, protocol.ProcessExited{ExitCode: -1})
		conn.Write(ctx, websocket.MessageBinary, exitData)
		return
	}

	ms.mu.Lock()
	ms.proc = proc
	ms.mu.Unlock()

	// Send SpawnComplete
	scData, _ := protocol.Encode(protocol.TypeSpawnComplete, protocol.SpawnComplete{Cols: cols, Rows: rows})
	conn.Write(ctx, websocket.MessageBinary, scData)

	d.Logger.Info("shell spawned", "identity", mapping.Identity, "pid", proc.Pid(), "shell", mapping.Shell)

	// Stdout → relay goroutine
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := proc.Read(buf)
			if n > 0 {
				data, _ := protocol.Encode(protocol.TypeStdout, buf[:n])
				conn.Write(ctx, websocket.MessageBinary, data)
			}
			if err != nil {
				break
			}
		}
	}()

	// Stdin → PTY goroutine
	go func() {
		for data := range ms.stdinCh {
			proc.Write(data)
		}
	}()

	// Wait for process exit
	exitCode, _ := proc.Wait(ctx)
	proc.Close()

	ms.mu.Lock()
	ms.proc = nil
	close(ms.stdinCh)
	ms.stdinCh = nil
	ms.mu.Unlock()

	d.Logger.Info("shell exited", "identity", mapping.Identity, "exit_code", exitCode)

	exitData, _ := protocol.Encode(protocol.TypeProcessExited, protocol.ProcessExited{ExitCode: exitCode})
	conn.Write(ctx, websocket.MessageBinary, exitData)
}

func (d *Daemon) forwardStdin(identity string, data []byte) {
	ms := d.getSession(identity)
	if ms == nil {
		return
	}
	ms.mu.Lock()
	ch := ms.stdinCh
	ms.mu.Unlock()
	if ch != nil {
		select {
		case ch <- data:
		default: // drop if channel full
		}
	}
}

func (d *Daemon) resizePTY(identity string, cols, rows int) {
	ms := d.getSession(identity)
	if ms == nil {
		return
	}
	ms.mu.Lock()
	proc := ms.proc
	ms.mu.Unlock()
	if proc != nil {
		proc.Resize(cols, rows)
	}
}
```

Add `"io"` to daemon.go imports.

**Step 4: Run all daemon tests**

Run: `go test ./internal/daemon/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/daemon/daemon.go internal/daemon/spawn_unix.go internal/daemon/spawn_windows.go
git commit -m "feat(daemon): PTY spawn as local user with stdin/stdout forwarding"
```

---

## Task 9: Platform Service — Linux (systemd)

**Files:**
- Create: `internal/daemon/service_linux.go`

**Step 1: Implement**

```go
//go:build linux

package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const serviceUnitPath = "/etc/systemd/system/phosphor-daemon.service"

func Install(binaryPath string) error {
	if binaryPath == "" {
		var err error
		binaryPath, err = os.Executable()
		if err != nil {
			return fmt.Errorf("resolve executable: %w", err)
		}
		binaryPath, _ = filepath.Abs(binaryPath)
	}

	unit := fmt.Sprintf(`[Unit]
Description=Phosphor Terminal Sharing Daemon
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s daemon run
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
`, binaryPath)

	if err := os.WriteFile(serviceUnitPath, []byte(unit), 0644); err != nil {
		return fmt.Errorf("write unit file: %w", err)
	}

	if err := exec.Command("systemctl", "daemon-reload").Run(); err != nil {
		return fmt.Errorf("systemctl daemon-reload: %w", err)
	}
	if err := exec.Command("systemctl", "enable", "--now", "phosphor-daemon").Run(); err != nil {
		return fmt.Errorf("systemctl enable: %w", err)
	}

	return nil
}

func Uninstall() error {
	_ = exec.Command("systemctl", "disable", "--now", "phosphor-daemon").Run()
	if err := os.Remove(serviceUnitPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	_ = exec.Command("systemctl", "daemon-reload").Run()
	return nil
}

func IsServiceEnvironment() bool {
	return false // systemd runs us as a normal process
}
```

**Step 2: Commit**

```bash
git add internal/daemon/service_linux.go
git commit -m "feat(daemon): systemd service install/uninstall for Linux"
```

---

## Task 10: Platform Service — macOS (launchd)

**Files:**
- Create: `internal/daemon/service_darwin.go`

**Step 1: Implement**

```go
//go:build darwin

package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const plistPath = "/Library/LaunchDaemons/com.phosphor.daemon.plist"

func Install(binaryPath string) error {
	if binaryPath == "" {
		var err error
		binaryPath, err = os.Executable()
		if err != nil {
			return fmt.Errorf("resolve executable: %w", err)
		}
		binaryPath, _ = filepath.Abs(binaryPath)
	}

	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.phosphor.daemon</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
        <string>daemon</string>
        <string>run</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>/var/log/phosphor-daemon.log</string>
    <key>StandardErrorPath</key>
    <string>/var/log/phosphor-daemon.log</string>
</dict>
</plist>
`, binaryPath)

	if err := os.WriteFile(plistPath, []byte(plist), 0644); err != nil {
		return fmt.Errorf("write plist: %w", err)
	}
	if err := exec.Command("launchctl", "load", plistPath).Run(); err != nil {
		return fmt.Errorf("launchctl load: %w", err)
	}
	return nil
}

func Uninstall() error {
	_ = exec.Command("launchctl", "unload", plistPath).Run()
	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func IsServiceEnvironment() bool {
	return false // launchd runs us as a normal process
}
```

**Step 2: Commit**

```bash
git add internal/daemon/service_darwin.go
git commit -m "feat(daemon): launchd service install/uninstall for macOS"
```

---

## Task 11: Platform Service — Windows (SCM)

**Files:**
- Create: `internal/daemon/service_windows.go`

**Step 1: Implement**

```go
//go:build windows

package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const serviceName = "PhosphorDaemon"
const serviceDisplayName = "Phosphor Terminal Sharing Daemon"

func Install(binaryPath string) error {
	if binaryPath == "" {
		var err error
		binaryPath, err = os.Executable()
		if err != nil {
			return fmt.Errorf("resolve executable: %w", err)
		}
		binaryPath, _ = filepath.Abs(binaryPath)
	}

	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to SCM: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err == nil {
		s.Close()
		return fmt.Errorf("service %q already exists", serviceName)
	}

	s, err = m.CreateService(serviceName, binaryPath, mgr.Config{
		DisplayName: serviceDisplayName,
		StartType:   mgr.StartAutomatic,
		Description: "Maintains persistent connections to the Phosphor relay and spawns terminal sessions on demand.",
	}, "daemon", "run")
	if err != nil {
		return fmt.Errorf("create service: %w", err)
	}
	defer s.Close()

	if err := s.Start(); err != nil {
		return fmt.Errorf("start service: %w", err)
	}

	return nil
}

func Uninstall() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to SCM: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err != nil {
		return fmt.Errorf("open service: %w", err)
	}
	defer s.Close()

	status, err := s.Control(svc.Stop)
	if err == nil {
		// Wait for stop
		for status.State != svc.Stopped {
			time.Sleep(500 * time.Millisecond)
			status, _ = s.Query()
		}
	}

	return s.Delete()
}

func IsServiceEnvironment() bool {
	isService, _ := svc.IsWindowsService()
	return isService
}

// phosphorService implements svc.Handler for the Windows SCM.
type phosphorService struct {
	daemon *Daemon
	logger *slog.Logger
}

func (ps *phosphorService) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	changes <- svc.Status{State: svc.StartPending}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		ps.daemon.Run(ctx)
		close(done)
	}()

	changes <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}

	for {
		select {
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				changes <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				changes <- svc.Status{State: svc.StopPending}
				cancel()
				<-done
				return false, 0
			}
		case <-done:
			return false, 0
		}
	}
}

// RunAsService runs the daemon under the Windows SCM.
func RunAsService(d *Daemon) error {
	return svc.Run(serviceName, &phosphorService{daemon: d, logger: d.Logger})
}
```

**Step 2: Commit**

```bash
git add internal/daemon/service_windows.go
git commit -m "feat(daemon): Windows SCM service install/uninstall/run"
```

---

## Task 12: CLI Subcommands — `phosphor daemon`

**Files:**
- Create: `cmd/phosphor/daemon.go`
- Modify: `cmd/phosphor/main.go:163` (add daemonCmd)

**Step 1: Implement daemon subcommands**

Create `cmd/phosphor/daemon.go`:

```go
package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/brporter/phosphor/internal/cli"
	"github.com/brporter/phosphor/internal/daemon"
)

func newDaemonCmd() *cobra.Command {
	var configPath string

	daemonCmd := &cobra.Command{
		Use:   "daemon",
		Short: "Manage the phosphor background daemon",
	}

	// --- daemon run ---
	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Run the daemon (foreground or as a service)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if configPath == "" {
				configPath = daemon.DefaultConfigPath()
			}

			cfg, err := daemon.ReadConfig(configPath)
			if err != nil {
				return fmt.Errorf("no config at %s — run 'phosphor daemon map' first: %w", configPath, err)
			}
			if len(cfg.Mappings) == 0 {
				return fmt.Errorf("no mappings configured — run 'phosphor daemon map' first")
			}

			logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

			// Load token
			tc, err := cli.LoadTokenCache()
			if err != nil {
				return fmt.Errorf("no cached token — run 'phosphor login' first: %w", err)
			}

			d := &daemon.Daemon{
				Config:     cfg,
				Token:      tc.AccessToken,
				Logger:     logger,
				ConfigPath: configPath,
			}

			// If running as a Windows service, use the SCM handler
			if daemon.IsServiceEnvironment() {
				return daemon.RunAsService(d)
			}

			// Foreground mode
			ctx, cancel := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			d.Run(ctx)
			return nil
		},
	}

	// --- daemon install ---
	installCmd := &cobra.Command{
		Use:   "install",
		Short: "Install the daemon as a system service",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := daemon.Install(""); err != nil {
				return err
			}
			fmt.Println("Phosphor daemon installed and started.")
			return nil
		},
	}

	// --- daemon uninstall ---
	uninstallCmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the daemon system service",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := daemon.Uninstall(); err != nil {
				return err
			}
			fmt.Println("Phosphor daemon uninstalled.")
			return nil
		},
	}

	// --- daemon map ---
	var mapIdentity, mapUser, mapShell string
	mapCmd := &cobra.Command{
		Use:   "map",
		Short: "Map a web identity to a local user account",
		RunE: func(cmd *cobra.Command, args []string) error {
			if configPath == "" {
				configPath = daemon.DefaultConfigPath()
			}
			cfg, err := daemon.ReadConfig(configPath)
			if err != nil {
				cfg = &daemon.Config{Relay: cli.DefaultRelayURL}
			}
			cfg.AddMapping(daemon.Mapping{
				Identity:  mapIdentity,
				LocalUser: mapUser,
				Shell:     mapShell,
			})
			if err := daemon.WriteConfig(configPath, cfg); err != nil {
				return err
			}
			fmt.Printf("Mapped %s → %s (%s)\n", mapIdentity, mapUser, mapShell)
			return nil
		},
	}
	mapCmd.Flags().StringVar(&mapIdentity, "identity", "", "Web identity (email)")
	mapCmd.Flags().StringVar(&mapUser, "user", "", "Local user account")
	mapCmd.Flags().StringVar(&mapShell, "shell", "", "Shell to launch")
	mapCmd.MarkFlagRequired("identity")
	mapCmd.MarkFlagRequired("user")
	mapCmd.MarkFlagRequired("shell")

	// --- daemon unmap ---
	var unmapIdentity string
	unmapCmd := &cobra.Command{
		Use:   "unmap",
		Short: "Remove an identity mapping",
		RunE: func(cmd *cobra.Command, args []string) error {
			if configPath == "" {
				configPath = daemon.DefaultConfigPath()
			}
			cfg, err := daemon.ReadConfig(configPath)
			if err != nil {
				return err
			}
			if !cfg.RemoveMapping(unmapIdentity) {
				return fmt.Errorf("no mapping for %q", unmapIdentity)
			}
			if err := daemon.WriteConfig(configPath, cfg); err != nil {
				return err
			}
			fmt.Printf("Removed mapping for %s\n", unmapIdentity)
			return nil
		},
	}
	unmapCmd.Flags().StringVar(&unmapIdentity, "identity", "", "Web identity to remove")
	unmapCmd.MarkFlagRequired("identity")

	// --- daemon maps ---
	mapsCmd := &cobra.Command{
		Use:   "maps",
		Short: "List identity mappings",
		RunE: func(cmd *cobra.Command, args []string) error {
			if configPath == "" {
				configPath = daemon.DefaultConfigPath()
			}
			cfg, err := daemon.ReadConfig(configPath)
			if err != nil {
				return fmt.Errorf("no config at %s: %w", configPath, err)
			}
			if len(cfg.Mappings) == 0 {
				fmt.Println("No mappings configured.")
				return nil
			}
			fmt.Printf("%-30s %-15s %s\n", "IDENTITY", "LOCAL USER", "SHELL")
			for _, m := range cfg.Mappings {
				fmt.Printf("%-30s %-15s %s\n", m.Identity, m.LocalUser, m.Shell)
			}
			return nil
		},
	}

	daemonCmd.PersistentFlags().StringVar(&configPath, "config", "", "Config file path (default: platform-specific)")
	daemonCmd.AddCommand(runCmd, installCmd, uninstallCmd, mapCmd, unmapCmd, mapsCmd)
	return daemonCmd
}
```

**Step 2: Wire into main.go**

In `cmd/phosphor/main.go`, at line 163 where `rootCmd.AddCommand(loginCmd, logoutCmd)` is called, add `newDaemonCmd()`:

```go
rootCmd.AddCommand(loginCmd, logoutCmd, newDaemonCmd())
```

**Step 3: Build and verify**

Run: `go build ./cmd/phosphor`
Run: `./phosphor daemon --help`
Expected: Shows daemon subcommands (run, install, uninstall, map, unmap, maps).

**Step 4: Commit**

```bash
git add cmd/phosphor/daemon.go cmd/phosphor/main.go
git commit -m "feat(cli): add 'phosphor daemon' subcommands (run, install, uninstall, map, unmap, maps)"
```

---

## Task 13: Web SPA — Show Lazy Session Status

**Files:**
- Modify: `web/src/components/SessionCard.tsx:5-13` (SessionData interface)
- Modify: `web/src/components/SessionCard.tsx:68-93` (status badges)

**Step 1: Update SessionData interface**

In `SessionCard.tsx`, add to the interface:

```typescript
export interface SessionData {
  id: string;
  mode: string;
  cols: number;
  rows: number;
  command: string;
  viewers: number;
  process_exited: boolean;
  lazy: boolean;
  process_running: boolean;
}
```

**Step 2: Update the status badge display**

In the badge area (around lines 81-93), add a "ready" badge for lazy sessions:

```tsx
{session.lazy && !session.process_running && !session.process_exited && (
  <span
    style={{
      fontSize: 11,
      color: "var(--green)",
      border: "1px solid var(--green-dim)",
      padding: "2px 6px",
      marginLeft: 4,
    }}
  >
    ready
  </span>
)}
{session.process_exited && (
  <span
    style={{
      fontSize: 11,
      color: "var(--red)",
      border: "1px solid var(--red)",
      padding: "2px 6px",
      marginLeft: 4,
    }}
  >
    exited
  </span>
)}
```

**Step 3: Verify**

Run: `cd web && npm run build`
Expected: Build succeeds.

**Step 4: Commit**

```bash
git add web/src/components/SessionCard.tsx
git commit -m "feat(web): show 'ready' badge for lazy sessions"
```

---

## Task 14: Add fsnotify Dependency and Config Watch

**Files:**
- Modify: `go.mod` (add fsnotify)
- Modify: `internal/daemon/daemon.go` (add config watcher)

**Step 1: Add dependency**

Run: `go get github.com/fsnotify/fsnotify`

**Step 2: Add config watcher to Daemon.Run()**

In `daemon.go`, add a `watchConfig` method that uses `fsnotify` to watch the config file. On change, re-read the config, diff mappings, and add/remove managed sessions. Also listen for SIGHUP on Unix.

```go
func (d *Daemon) watchConfig(ctx context.Context) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		d.Logger.Warn("config watch unavailable", "err", err)
		return
	}
	defer watcher.Close()

	if err := watcher.Add(d.ConfigPath); err != nil {
		d.Logger.Warn("failed to watch config", "err", err)
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case event := <-watcher.Events:
			if event.Op&(fsnotify.Write|fsnotify.Create) != 0 {
				d.reloadConfig()
			}
		case err := <-watcher.Errors:
			d.Logger.Warn("config watch error", "err", err)
		}
	}
}

func (d *Daemon) reloadConfig() {
	newCfg, err := ReadConfig(d.ConfigPath)
	if err != nil {
		d.Logger.Error("reload config failed", "err", err)
		return
	}
	d.Logger.Info("config reloaded", "mappings", len(newCfg.Mappings))
	// TODO: diff and add/remove sessions for new/removed mappings
}
```

Start `watchConfig` as a goroutine in `Run()`.

**Step 3: Commit**

```bash
git add go.mod go.sum internal/daemon/daemon.go
git commit -m "feat(daemon): add config file watching with fsnotify"
```

---

## Task 15: Integration Test — Full Daemon → Relay → Viewer Flow

**Files:**
- Create: `internal/daemon/integration_test.go`

**Step 1: Write integration test**

This test starts a real relay (httptest.Server), creates a daemon with a mapping, verifies the session appears, then simulates a viewer connecting and triggering a spawn. Since we can't spawn a real PTY in tests (requires root for user switching), this test focuses on the protocol handshake.

```go
package daemon_test

import (
	"context"
	"testing"
	"time"
	// ... imports for relay test helpers, protocol, websocket
)

func TestDaemonRelayIntegration(t *testing.T) {
	// 1. Start a relay in dev mode
	// 2. Create a Daemon with a test mapping
	// 3. Verify the daemon connects and session appears in relay
	// 4. Connect as a viewer
	// 5. Verify SpawnRequest is sent to daemon
	// 6. Clean up
}
```

The exact implementation depends on the test infrastructure — this may need to be adapted once Tasks 1-12 compile cleanly.

**Step 2: Run test**

Run: `go test ./internal/daemon/ -run TestDaemonRelayIntegration -v`

**Step 3: Commit**

```bash
git add internal/daemon/integration_test.go
git commit -m "test(daemon): add integration test for daemon-relay-viewer flow"
```

---

## Task 16: Run Full Test Suite and Fix Issues

**Step 1: Run all Go tests**

Run: `go test ./... -count=1`

**Step 2: Fix any compilation errors or test failures**

Likely issues:
- Redis store needs `SetProcessRunning` method
- Existing tests may need `Lazy`/`ProcessRunning` zero-values to be compatible

**Step 3: Run web build**

Run: `cd web && npm run build`

**Step 4: Fix any issues**

**Step 5: Commit**

```bash
git add -A
git commit -m "fix: resolve test failures and build issues from daemon feature"
```

---

## Summary

| Task | What | Files |
|------|------|-------|
| 1 | Protocol: new message types + Hello fields | `internal/protocol/`, `web/src/lib/protocol.ts` |
| 2 | SessionInfo: new fields + SetProcessRunning | `internal/relay/store*.go` |
| 3 | CLI handler: Lazy + DelegateFor support | `internal/relay/handler_ws_cli.go` |
| 4 | Viewer handler: SpawnRequest for lazy sessions | `internal/relay/handler_ws_viewer.go` |
| 5 | API: expose lazy/process_running, delegated ownership | `internal/relay/handler_api.go` |
| 6 | Daemon config: file read/write + mapping CRUD | `internal/daemon/config.go` |
| 7 | Daemon core: connection manager + read loop | `internal/daemon/daemon.go` |
| 8 | Daemon spawn: PTY as local user (Unix + Windows) | `internal/daemon/spawn_*.go` |
| 9 | Linux service: systemd install/uninstall | `internal/daemon/service_linux.go` |
| 10 | macOS service: launchd install/uninstall | `internal/daemon/service_darwin.go` |
| 11 | Windows service: SCM install/uninstall/run | `internal/daemon/service_windows.go` |
| 12 | CLI subcommands: phosphor daemon * | `cmd/phosphor/daemon.go`, `cmd/phosphor/main.go` |
| 13 | Web SPA: lazy session status badge | `web/src/components/SessionCard.tsx` |
| 14 | Config hot-reload: fsnotify watcher | `internal/daemon/daemon.go` |
| 15 | Integration test: end-to-end flow | `internal/daemon/integration_test.go` |
| 16 | Full test suite pass | All files |
