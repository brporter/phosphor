# Process Exit & Restart Design

## Problem

When the remote user types `exit` (or the subprocess exits for any reason), the CLI closes the WebSocket and the relay treats it as a network disconnect. The session enters a 60-second grace period and then is removed. There is no way to restart the process without creating a new session.

## Goal

When the subprocess exits, keep the session alive and allow the user to restart the original command by clicking the session in the web UI.

## Design Decisions

- **The original CLI re-executes the command** — the relay never runs processes.
- **Restart mode is configurable** via `--restart` flag: `manual` (default), `auto`, `never`.
- **No timeout** — CLI waits indefinitely for a restart signal in manual mode.
- **Viewer triggers restart** by clicking the session in the session list (normal join flow).

## Protocol Additions

Two new message types:

| Type | Direction | Byte | Payload |
|------|-----------|------|---------|
| `TypeProcessExited` | CLI → relay | `0x16` | `{ "exit_code": int }` |
| `TypeRestart` | relay → CLI | `0x17` | (none) |

## Flow: Manual Restart

1. Subprocess exits (e.g. user types `exit`)
2. CLI sends `TypeProcessExited { exit_code: 0 }` to relay, keeps WebSocket open
3. Relay sets `ProcessExited = true` on the session, broadcasts `TypeProcessExited` to all viewers
4. Viewers display `[Process exited (code 0)]` banner in terminal, session list shows "exited" status
5. User clicks the session in the session list, navigating to the viewer
6. Viewer join triggers relay to send `TypeRestart` to CLI
7. Relay sets `ProcessExited = false`, broadcasts `TypeRestart` to all viewers
8. CLI receives `TypeRestart`, re-spawns the subprocess, resumes I/O bridge
9. Viewers see new terminal output

## Flow: Auto Restart

1. Subprocess exits
2. CLI re-spawns the subprocess immediately (no `TypeProcessExited` sent, no relay involvement)
3. Viewers see new output seamlessly

## Flow: Never Restart (Current Behavior)

1. Subprocess exits
2. CLI closes WebSocket and exits
3. Relay runs the existing disconnect → grace period → unregister flow

## Component Changes

### CLI (`internal/cli/`)

- New `--restart` flag: `manual` | `auto` | `never` (default: `manual`)
- On process exit with `--restart=manual`: send `TypeProcessExited`, enter wait loop listening for `TypeRestart` on the WebSocket
- On receiving `TypeRestart`: re-spawn subprocess, resume I/O bridge
- On `--restart=auto`: re-spawn immediately in a loop, no protocol change
- On `--restart=never`: current behavior (`errProcessExited` → exit)

### Protocol (`internal/protocol/`, `web/src/lib/protocol.ts`)

- Add `TypeProcessExited = 0x16` and `TypeRestart = 0x17`
- Add `ProcessExited` struct with `ExitCode int`
- Keep both Go and TypeScript definitions in sync

### Relay (`internal/relay/`)

- `SessionInfo`: add `ProcessExited bool` field
- `SessionStore`: add `SetProcessExited(ctx, sessionID string, exited bool) error`
- `MemorySessionStore` / `RedisSessionStore`: implement `SetProcessExited`
- `handler_ws_cli.go`: handle `TypeProcessExited` in the read loop — update store, broadcast to viewers
- `handler_ws_viewer.go`: on viewer join, if `ProcessExited == true`, send `TypeRestart` to CLI and clear the flag
- `Hub`: add `RestartProcess(ctx, sessionID)` method that sends `TypeRestart` to the local CLI session and clears the `ProcessExited` flag

### Web (`web/src/`)

- `protocol.ts`: add `MsgType.ProcessExited` and `MsgType.Restart`
- `useWebSocket.ts`: handle `ProcessExited` (set state, show banner), handle `Restart` (clear state)
- `TerminalView.tsx`: show `[Process exited (code N)]` banner when `processExited` is true
- Session list: show "exited" badge for sessions where `ProcessExited == true`

### Data Store

**Memory store**: add `ProcessExited` field to `SessionInfo` struct (already in-memory).

**Redis store**: add `process_exited` field to the `session:{id}` hash.
