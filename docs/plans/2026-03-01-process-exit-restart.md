# Process Exit & Restart Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** When a subprocess exits, keep the CLI session alive so the user can restart the process from the web UI.

**Architecture:** Two new protocol messages (`TypeProcessExited` CLI→relay, `TypeRestart` relay→CLI) let the CLI signal process death and wait for a restart signal. The relay tracks `ProcessExited` state in the session store and triggers restart when a viewer joins an exited session. The CLI `--restart` flag controls behavior: `manual` (default), `auto`, `never`.

**Tech Stack:** Go (CLI + relay), TypeScript/React (web), binary WebSocket protocol

**Design doc:** `docs/plans/2026-03-01-process-exit-restart-design.md`

---

### Task 1: Add Protocol Messages (Go)

**Files:**
- Modify: `internal/protocol/messages.go`

**Note:** The design doc says `0x16`/`0x17` but `TypeError` is already `0x16`. Use `0x17` and `0x18`.

**Step 1: Add type constants and struct**

Add after `TypePong` (line 18):

```go
TypeProcessExited byte = 0x17
TypeRestart       byte = 0x18
```

Add after the `Reconnect` struct (line 81):

```go
// ProcessExited is sent by the CLI when the subprocess exits.
type ProcessExited struct {
	ExitCode int `json:"exit_code"`
}
```

**Step 2: Verify build**

Run: `go build ./internal/protocol/...`
Expected: success

**Step 3: Commit**

```bash
git add internal/protocol/messages.go
git commit -m "feat(protocol): add TypeProcessExited and TypeRestart message types"
```

---

### Task 2: Add Protocol Messages (TypeScript)

**Files:**
- Modify: `web/src/lib/protocol.ts`

**Step 1: Add message types and payload interface**

Add to `MsgType` object (after `Pong: 0x31` at line 18):

```typescript
ProcessExited: 0x17,
Restart: 0x18,
```

Add after `ReconnectPayload` (line 42):

```typescript
export interface ProcessExitedPayload {
  exit_code: number;
}
```

**Step 2: Verify build**

Run: `cd web && npx tsc --noEmit`
Expected: success

**Step 3: Commit**

```bash
git add web/src/lib/protocol.ts
git commit -m "feat(web): add ProcessExited and Restart protocol types"
```

---

### Task 3: Add `ProcessExited` to SessionInfo and Store Interface

**Files:**
- Modify: `internal/relay/store.go`

**Step 1: Add field to SessionInfo**

Add after `DisconnectedAt time.Time` (line 23):

```go
ProcessExited bool
```

**Step 2: Add method to SessionStore interface**

Add after `CancelExpiry` (line 36):

```go
SetProcessExited(ctx context.Context, sessionID string, exited bool) error
```

**Step 3: Verify build**

Run: `go build ./internal/relay/...`
Expected: FAIL — MemorySessionStore and RedisSessionStore don't implement the new method yet.

**Step 4: Commit**

```bash
git add internal/relay/store.go
git commit -m "feat(store): add ProcessExited field and SetProcessExited to interface"
```

---

### Task 4: Implement SetProcessExited in MemorySessionStore

**Files:**
- Modify: `internal/relay/store_memory.go`

**Step 1: Add SetProcessExited method**

Add after `CancelExpiry` method (after line 142):

```go
func (m *MemorySessionStore) SetProcessExited(_ context.Context, sessionID string, exited bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	info, ok := m.sessions[sessionID]
	if !ok {
		return nil
	}
	info.ProcessExited = exited
	m.sessions[sessionID] = info
	return nil
}
```

**Step 2: Verify build**

Run: `go build ./internal/relay/...`
Expected: FAIL — RedisSessionStore still doesn't implement it.

**Step 3: Commit**

```bash
git add internal/relay/store_memory.go
git commit -m "feat(store): implement SetProcessExited for MemorySessionStore"
```

---

### Task 5: Implement SetProcessExited in RedisSessionStore

**Files:**
- Modify: `internal/relay/store_redis.go`

**Step 1: Add SetProcessExited method**

Add after `CancelExpiry` method (after line 174):

```go
func (s *RedisSessionStore) SetProcessExited(ctx context.Context, sessionID string, exited bool) error {
	val := "false"
	if exited {
		val = "true"
	}
	return s.rdb.HSet(ctx, sessionKey(sessionID), "process_exited", val).Err()
}
```

**Step 2: Update Register to include `process_exited` field**

In `Register` method (line 51), add to the `HSet` map:

```go
"process_exited": "false",
```

**Step 3: Update Get to read `process_exited` field**

In `Get` method, after parsing `disconnected` (line 100), add:

```go
processExited := vals["process_exited"] == "true"
```

And add to the returned `SessionInfo` struct (after `DisconnectedAt`):

```go
ProcessExited: processExited,
```

**Step 4: Verify build**

Run: `go build ./internal/relay/...`
Expected: success (both stores now implement the interface)

**Step 5: Commit**

```bash
git add internal/relay/store_redis.go
git commit -m "feat(store): implement SetProcessExited for RedisSessionStore"
```

---

### Task 6: Add RestartProcess to Hub

**Files:**
- Modify: `internal/relay/hub.go`

**Step 1: Add RestartProcess method**

Add after `Reconnect` method (after line 155):

```go
// RestartProcess sends TypeRestart to the CLI and clears the ProcessExited flag.
func (h *Hub) RestartProcess(ctx context.Context, sessionID string) error {
	h.store.SetProcessExited(ctx, sessionID, false)

	data, err := protocol.Encode(protocol.TypeRestart, nil)
	if err != nil {
		return err
	}

	// Send to local CLI
	if ls, ok := h.GetLocal(sessionID); ok && ls.HasCLI() {
		ls.SendToCLI(ctx, data)
	} else if h.bus != nil {
		// CLI on remote relay
		h.bus.Publish(ctx, InputChannel(sessionID), data)
	}

	// Broadcast restart to all viewers
	h.BroadcastOutput(ctx, sessionID, data)
	return nil
}
```

**Step 2: Verify build**

Run: `go build ./internal/relay/...`
Expected: success

**Step 3: Commit**

```bash
git add internal/relay/hub.go
git commit -m "feat(hub): add RestartProcess method"
```

---

### Task 7: Handle TypeProcessExited in CLI Read Loop (Relay)

**Files:**
- Modify: `internal/relay/handler_ws_cli.go`

**Step 1: Add TypeProcessExited case to the read loop**

In `HandleCLIWebSocket`, inside the `switch msgType` block (after `case protocol.TypePong:` at line 155), add:

```go
case protocol.TypeProcessExited:
	s.hub.store.SetProcessExited(ctx, sessionID, true)
	encoded, err := protocol.Encode(protocol.TypeProcessExited, payload)
	if err == nil {
		s.hub.BroadcastOutput(ctx, sessionID, encoded)
	}
	s.logger.Info("process exited", "session", sessionID)
```

**Step 2: Verify build**

Run: `go build ./internal/relay/...`
Expected: success

**Step 3: Commit**

```bash
git add internal/relay/handler_ws_cli.go
git commit -m "feat(relay): handle TypeProcessExited in CLI WebSocket handler"
```

---

### Task 8: Trigger Restart on Viewer Join

**Files:**
- Modify: `internal/relay/handler_ws_viewer.go`

**Step 1: After viewer joins an exited session, trigger restart**

In `HandleViewerWebSocket`, after sending the `Joined` message and notifying viewer count (after line 103), add:

```go
// If process has exited, trigger a restart
if info.ProcessExited {
	s.hub.RestartProcess(ctx, join.SessionID)
	s.logger.Info("viewer triggered process restart", "session", sessionID)
}
```

**Step 2: Verify build**

Run: `go build ./internal/relay/...`
Expected: success

**Step 3: Commit**

```bash
git add internal/relay/handler_ws_viewer.go
git commit -m "feat(relay): trigger process restart when viewer joins exited session"
```

---

### Task 9: Add `process_exited` to Session List API

**Files:**
- Modify: `internal/relay/handler_api.go`

**Step 1: Add `ProcessExited` field to sessionListItem**

Add to `sessionListItem` struct (after `Viewers` at line 15):

```go
ProcessExited bool `json:"process_exited"`
```

**Step 2: Set the field in HandleListSessions**

In the loop building `infos`, add to the struct literal (after `Viewers: viewers`):

```go
ProcessExited: info.ProcessExited,
```

**Step 3: Verify build**

Run: `go build ./internal/relay/...`
Expected: success

**Step 4: Commit**

```bash
git add internal/relay/handler_api.go
git commit -m "feat(api): include process_exited in session list response"
```

---

### Task 10: Add `--restart` Flag to CLI

**Files:**
- Modify: `internal/cli/config.go`
- Modify: `cmd/phosphor/main.go`

**Step 1: Add Restart field to App struct**

In `internal/cli/app.go`, add to the `App` struct (after `Mode string` at line 24):

```go
Restart string // "manual", "auto", "never"
```

**Step 2: Add `--restart` flag to cobra command**

In `cmd/phosphor/main.go`, add a variable before `rootCmd` (after `var token string` at line 15):

```go
var restart string
```

In the `RunE` function, set the `Restart` field on the `App` struct (after `Mode: mode`):

```go
Restart: restart,
```

After the existing flags (after line 74), add:

```go
rootCmd.Flags().StringVar(&restart, "restart", "manual", "Process restart mode: manual, auto, never")
```

**Step 3: Verify build**

Run: `go build ./cmd/phosphor/...`
Expected: success

**Step 4: Commit**

```bash
git add internal/cli/app.go cmd/phosphor/main.go
git commit -m "feat(cli): add --restart flag (manual, auto, never)"
```

---

### Task 11: Implement Process Exit & Restart in CLI

This is the core CLI change. The `Run()` loop and `runConnection()` need restructuring.

**Files:**
- Modify: `internal/cli/app.go`

**Step 1: Modify `Run()` to handle restart modes**

The current code starts the PTY once, then enters a reconnection loop. For process restart, we need an outer loop around PTY creation.

Replace the `Run()` method with:

```go
func (a *App) Run(ctx context.Context) error {
	appCtx, appCancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer appCancel()

	for {
		err := a.runWithProcess(appCtx)
		if err == nil || appCtx.Err() != nil {
			return nil
		}
		if !errors.Is(err, errProcessExited) {
			return err
		}

		// Process exited — decide what to do based on restart mode
		switch a.Restart {
		case "never", "":
			a.Logger.Info("session ended")
			return nil
		case "auto":
			a.Logger.Info("process exited, restarting automatically")
			fmt.Fprintf(os.Stderr, "Process exited. Restarting...\n")
			continue
		case "manual":
			// errProcessExited with manual mode is handled inside runWithProcess
			// (it waits for TypeRestart before returning nil)
			a.Logger.Info("session ended")
			return nil
		default:
			a.Logger.Info("session ended")
			return nil
		}
	}
}
```

**Step 2: Extract `runWithProcess()` — PTY lifecycle + reconnection loop**

Add a new method that wraps PTY creation and the reconnection loop:

```go
func (a *App) runWithProcess(appCtx context.Context) error {
	var cols, rows int
	var proc io.ReadWriteCloser
	var ptyProc PTYProcess

	if a.Mode == "pty" {
		p, c, r, err := StartPTY(a.Command)
		if err != nil {
			return fmt.Errorf("start pty: %w", err)
		}
		defer p.Close()
		proc = p
		ptyProc = p
		cols = c
		rows = r
	} else {
		proc = NewPipeReader(os.Stdin)
		cols = 80
		rows = 24
	}

	var sessionID, reconnectToken string
	attempt := 0

	for {
		result := a.runConnection(appCtx, proc, ptyProc, cols, rows, &sessionID, &reconnectToken)
		if result.processExited {
			if a.Restart == "manual" && result.ws != nil {
				// Send ProcessExited to relay, then wait for TypeRestart
				exitPayload := protocol.ProcessExited{ExitCode: result.exitCode}
				result.ws.Send(appCtx, protocol.TypeProcessExited, exitPayload)
				fmt.Fprintf(os.Stderr, "Process exited (code %d). Waiting for restart...\n", result.exitCode)
				a.Logger.Info("waiting for restart signal", "exit_code", result.exitCode)

				// Wait for TypeRestart
				for {
					mt, _, recvErr := result.ws.Receive(appCtx)
					if recvErr != nil {
						return fmt.Errorf("connection lost while waiting for restart")
					}
					if mt == protocol.TypeRestart {
						a.Logger.Info("restart signal received")
						fmt.Fprintf(os.Stderr, "Restart signal received. Restarting process...\n")
						return errProcessExited // triggers auto restart in Run()
					}
					if mt == protocol.TypePing {
						result.ws.Send(appCtx, protocol.TypePong, nil)
					}
				}
			}
			if a.Restart == "auto" {
				return errProcessExited
			}
			// never mode or no ws
			return nil
		}
		if result.err == nil || appCtx.Err() != nil {
			return nil
		}

		delay := backoffSchedule[min(attempt, len(backoffSchedule)-1)]
		attempt++
		fmt.Fprintf(os.Stderr, "Relay disconnected. Reconnecting in %s...\n", delay)
		a.Logger.Info("relay disconnected, will retry", "delay", delay, "attempt", attempt)
		select {
		case <-time.After(delay):
		case <-appCtx.Done():
			return nil
		}
	}
}
```

**Step 3: Modify `runConnection()` to return structured result**

Change `runConnection` to return a struct instead of a plain error:

```go
type connectionResult struct {
	err          error
	processExited bool
	exitCode     int
	ws           *WSConn // keep WebSocket alive for manual restart
}
```

Update the `runConnection` signature:

```go
func (a *App) runConnection(
	appCtx context.Context,
	proc io.ReadWriteCloser,
	ptyProc PTYProcess,
	cols, rows int,
	sessionID, reconnectToken *string,
) connectionResult {
```

At the end of `runConnection`, replace the `<-connCtx.Done()` block with:

```go
<-connCtx.Done()
select {
case <-procDead:
	return connectionResult{processExited: true, exitCode: 0, ws: ws}
default:
	return connectionResult{err: fmt.Errorf("connection lost")}
}
```

And also move the `defer ws.Close()` logic — don't close `ws` if process exited and restart mode is manual. Instead, only defer close if the result doesn't need the connection:

Remove `defer ws.Close()` from line 109. The caller (`runWithProcess`) will be responsible for closing when done.

**Step 4: Verify build**

Run: `go build ./cmd/phosphor/...`
Expected: success

**Step 5: Commit**

```bash
git add internal/cli/app.go
git commit -m "feat(cli): implement process exit detection and restart wait loop"
```

---

### Task 12: Handle ProcessExited and Restart in Web Viewer

**Files:**
- Modify: `web/src/hooks/useWebSocket.ts`
- Modify: `web/src/components/TerminalView.tsx`

**Step 1: Add processExited state to useWebSocket**

In `useWebSocket.ts`, import `ProcessExitedPayload`:

```typescript
import {
  MsgType,
  decode,
  decodeJSON,
  encode,
  type JoinedPayload,
  type ErrorPayload,
  type ReconnectPayload,
  type ProcessExitedPayload,
} from "../lib/protocol";
```

Add state:

```typescript
const [processExited, setProcessExited] = useState<number | null>(null);
```

Add cases to the message switch (after `MsgType.Reconnect` case):

```typescript
case MsgType.ProcessExited: {
  const info = decodeJSON<ProcessExitedPayload>(payload);
  setProcessExited(info.exit_code);
  break;
}
case MsgType.Restart:
  setProcessExited(null);
  break;
```

Add `processExited` to the return object:

```typescript
return { connected, joined, error, processExited, sendStdin, sendResize };
```

**Step 2: Show exit/restart banner in TerminalView**

In `TerminalView.tsx`, destructure `processExited` from useWebSocket:

```typescript
const { connected, joined, error, processExited, sendStdin, sendResize } = useWebSocket({...});
```

Write to terminal when processExited changes:

```typescript
useEffect(() => {
  if (processExited !== null) {
    termRef.current?.write(
      `\r\n\x1b[1;33m[Process exited (code ${processExited}). Click session in list to restart.]\x1b[0m\r\n`
    );
  }
}, [processExited]);
```

Update the status bar to show process exited state (in the status div, add before the `ended` check):

```typescript
{processExited !== null ? (
  <span style={{ color: "var(--amber)" }}>process exited ({processExited})</span>
) : ended ? (
```

**Step 3: Verify build**

Run: `cd web && npx tsc --noEmit`
Expected: success

**Step 4: Commit**

```bash
git add web/src/hooks/useWebSocket.ts web/src/components/TerminalView.tsx
git commit -m "feat(web): handle ProcessExited and Restart in viewer"
```

---

### Task 13: Show Exited Badge in Session List

**Files:**
- Modify: `web/src/components/SessionCard.tsx`

**Step 1: Add `process_exited` to SessionData**

In the `SessionData` interface, add:

```typescript
process_exited: boolean;
```

**Step 2: Show "exited" badge in SessionCard**

After the mode badge span (line 52), add:

```typescript
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

**Step 3: Verify build**

Run: `cd web && npx tsc --noEmit`
Expected: success

**Step 4: Commit**

```bash
git add web/src/components/SessionCard.tsx
git commit -m "feat(web): show exited badge in session list"
```

---

### Task 14: Full Build & Smoke Test

**Step 1: Build everything**

Run: `make`
Expected: All three components build successfully.

**Step 2: Run Go tests**

Run: `go test ./... -count=1`
Expected: All tests pass.

**Step 3: Single-instance smoke test**

In terminal 1: `DEV_MODE=1 make dev-relay`
In terminal 2: `make dev-web`
In terminal 3: `./bin/phosphor --token dev --relay ws://localhost:8080 -- bash`

1. Open browser at `http://localhost:3000`, click the session
2. In the terminal view, type `exit`
3. Verify: terminal shows `[Process exited (code 0). Click session in list to restart.]`
4. Verify: session list shows "exited" badge
5. Click session in list again
6. Verify: process restarts, new shell prompt appears
7. Verify: CLI stderr shows restart messages

**Step 4: Test auto restart mode**

In terminal 3: `./bin/phosphor --token dev --relay ws://localhost:8080 --restart auto -- bash`

1. Type `exit` in the viewer
2. Verify: new shell prompt appears almost immediately without clicking

**Step 5: Test never restart mode**

In terminal 3: `./bin/phosphor --token dev --relay ws://localhost:8080 --restart never -- bash`

1. Type `exit` in the viewer
2. Verify: CLI exits, session disappears after grace period

**Step 6: Commit if any fixes were needed**

```bash
git add -A
git commit -m "fix: address issues found during smoke testing"
```
