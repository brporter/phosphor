# Daemon / Windows Service Design

## Overview

A background daemon (Linux/macOS) and Windows service that maintains persistent connections to the phosphor relay server, advertising phantom sessions that are created on-demand when a viewer connects via the web interface. Supports multi-user systems by mapping authenticated web identities to local OS user accounts.

## Approach

Subcommand of the existing `phosphor` binary (`phosphor daemon run|install|uninstall|map|unmap|maps`). Single binary distribution. On Windows, the same binary acts as both CLI and SCM service.

## Protocol Changes

### New Message Types

- `TypeSpawnRequest = 0x22` (relay → CLI): sent when the first viewer joins a lazy session with no running process. Empty payload.
- `TypeSpawnComplete = 0x23` (CLI → relay): sent after the daemon spawns the PTY. Payload: `{Cols, Rows}`.

### Hello Message Additions

- `Lazy bool` — session is on-demand; no process until a viewer connects.
- `DelegateFor string` — email or "provider:sub" of the identity that should own this session. Enables a service account to create sessions on behalf of mapped users.

### SessionInfo Additions

- `Lazy bool`
- `ProcessRunning bool` — distinguishes "never started" from "started and exited"
- `DelegateFor string` — delegated owner identity
- `ServiceIdentity string` — the daemon's own provider:sub (for auditing)

### /api/sessions Response Additions

- `lazy` (bool)
- `process_running` (bool)

## Multi-User Architecture

### Config File

Location: `/etc/phosphor/daemon.json` (Linux/macOS), `C:\ProgramData\phosphor\daemon.json` (Windows).

```json
{
  "relay": "wss://phosphor.example.com",
  "mappings": [
    {
      "identity": "bryan@bryanporter.com",
      "local_user": "brporter",
      "shell": "/bin/bash"
    },
    {
      "identity": "alice@example.com",
      "local_user": "alice",
      "shell": "/bin/zsh"
    }
  ]
}
```

### Identity Mapping

The `identity` field matches against the JWT `email` claim (or `sub` if no email). The daemon opens one WebSocket connection per mapping, each advertising a phantom session with `DelegateFor` set to the mapping's identity.

### CLI Subcommands

- `phosphor daemon map --identity <email> --user <local_user> --shell <shell>` — add/update a mapping
- `phosphor daemon unmap --identity <email>` — remove a mapping
- `phosphor daemon maps` — list all current mappings

These edit the config file directly. Require elevated privileges.

### Spawning as Another User

- **Linux/macOS:** `syscall.SysProcAttr{Credential: &syscall.Credential{Uid, Gid}}` on exec.Cmd before starting the PTY. Sets `HOME`, `USER`, `SHELL` env vars. Requires root.
- **Windows:** `LogonUser` to obtain a token for the local user, then `CreateProcessAsUser` via `syscall.SysProcAttr{Token: userToken}`. Requires SYSTEM or appropriate privileges.

## Daemon Core

### Package: `internal/daemon/`

Cross-platform daemon core managing the connect → idle → spawn → active → teardown → idle lifecycle.

### Lifecycle (per mapping)

1. **Connect** — WebSocket to `/ws/cli`, send `Hello{Lazy: true, Command: shell, DelegateFor: identity}`
2. **Idle** — Keep-alive only (Ping/Pong). No PTY, no subprocess. Minimal resources.
3. **Spawn** — On `TypeSpawnRequest`, call `StartPTY(shell)` as the mapped local user, send `TypeSpawnComplete{Cols, Rows}`, begin forwarding stdout.
4. **Active** — Forward stdin/stdout, handle resize. Identical to current CLI.
5. **Teardown** — Shell exits → `TypeProcessExited` → close PTY → return to Idle. WebSocket stays connected.
6. **Reconnect** — On WS drop, reconnect with `SessionID` + `ReconnectToken`, return to Idle.

### Authentication

The daemon reads the cached token from `~/.config/phosphor/tokens.json` (provisioned by running `phosphor login` beforehand). The daemon authenticates with this single identity; the `DelegateFor` field in Hello delegates session ownership to the mapped user.

### Config Hot-Reload

- Linux/macOS: `SIGHUP` triggers reload
- All platforms: `fsnotify` file watcher as fallback
- On reload: diff current vs new mappings. Close WS connections for removed mappings, open new ones for additions. Active sessions are not interrupted.

## Relay-Side Changes

### CLI Handler (`handler_ws_cli.go`)

- On Hello with `Lazy: true`: register session with `ProcessRunning: false`
- On Hello with `DelegateFor`: verify the connecting identity has service privilege, store `DelegateFor` as session owner, store authenticating identity as `ServiceIdentity`

### Viewer Handler (`handler_ws_viewer.go`)

- On viewer Join where `info.Lazy && !info.ProcessRunning`: send `TypeSpawnRequest` to CLI connection
- On `TypeSpawnComplete` from CLI: update dimensions, set `ProcessRunning: true`, send `TypeJoined` to viewer
- Guard against duplicate `TypeSpawnRequest` (mutex/flag) when multiple viewers connect simultaneously

### API Handler (`handler_api.go`)

- Ownership check uses `DelegateFor` identity when present

## Platform Service Integration

### Subcommand Structure

```
phosphor daemon run        # run in foreground (all platforms)
phosphor daemon install    # install as system service
phosphor daemon uninstall  # remove system service
phosphor daemon map ...    # add identity mapping
phosphor daemon unmap ...  # remove identity mapping
phosphor daemon maps       # list mappings
```

### Linux (`service_linux.go`)

- `Install()`: writes systemd unit to `/etc/systemd/system/phosphor-daemon.service`, runs `systemctl daemon-reload && systemctl enable --now`
- `Uninstall()`: `systemctl disable --now`, removes unit file
- Unit: `Type=simple`, `ExecStart=phosphor daemon run`, `Restart=always`

### macOS (`service_darwin.go`)

- `Install()`: writes launchd plist to `/Library/LaunchDaemons/com.phosphor.daemon.plist`, runs `launchctl load`
- `Uninstall()`: `launchctl unload`, removes plist
- Plist: `RunAtLoad=true`, `KeepAlive=true`, `UserName=root`

### Windows (`service_windows.go`)

- `Install()`: creates SCM service via `golang.org/x/sys/windows/svc/mgr` — `StartType=Automatic`, `Account=LocalSystem`
- `Uninstall()`: stops and deletes via SCM
- `Run()`: detects SCM via `svc.IsWindowsService()` — if true, dispatches to service handler; if false, runs foreground
- ConPTY works headless in Session 0 (no visible console)

## Web SPA Changes

- `SessionCard.tsx`: show status — "Ready" (lazy, not running), "Active" (running), "Exited" (process_exited)
- Clicking a "Ready" session navigates to `/session/:id` as normal; viewer WS connection triggers the spawn

## Error Handling

- **Spawn failure:** send `TypeProcessExited{ExitCode: -1}`, return to idle. Don't crash the daemon.
- **Token expiry:** log clear error, back off and retry every 5 minutes.
- **Invalid mapping (user doesn't exist):** log warning at config load, skip that mapping, don't block others.
- **Multiple viewers, same session:** first viewer triggers spawn, subsequent viewers get scrollback.
- **Shell exits while viewers connected:** `TypeProcessExited` to all viewers, daemon returns to idle.
- **Daemon shutdown:** gracefully close PTYs (SIGHUP on Unix, terminate tree on Windows), send `TypeProcessExited`, close WS connections.
- **Config file missing:** `daemon run` exits with clear error; `daemon map` creates file if absent.
- **Race (simultaneous viewer connects):** relay uses mutex/flag to send only one `TypeSpawnRequest`.

## File Structure

```
internal/daemon/
  daemon.go          — Daemon struct, per-mapping lifecycle loop
  config.go          — Config/Mapping structs, read/write/CRUD
  spawn_unix.go      — StartPTYAsUser (Credential, env setup)
  spawn_windows.go   — StartPTYAsUser (LogonUser, CreateProcessAsUser)
  service.go         — Service interface (Install/Uninstall)
  service_linux.go   — systemd integration
  service_darwin.go  — launchd integration
  service_windows.go — Windows SCM integration
cmd/phosphor/
  daemon.go          — cobra subcommands (run, install, uninstall, map, unmap, maps)
```
