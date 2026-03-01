# iOS Process Exit & Restart Support

## Summary

Port the web app's process exit/restart handling to the iOS app. The server-side logic already exists; this adds client-side awareness of `ProcessExited` (0x17) and `Restart` (0x18) protocol messages.

## Changes by Layer

### Protocol Layer (`Protocol/`)

- Add `MessageType.processExited = 0x17` and `MessageType.restart = 0x18`
- Add `ProcessExitedPayload` struct with `exitCode: Int` (CodingKeys: `exit_code`)

### WebSocket Layer (`Services/WebSocketManager.swift`)

- Add `WebSocketEvent.processExited(Int)` and `WebSocketEvent.restart` cases
- Handle both in `handleBinaryMessage()`: decode ProcessExited JSON payload, yield events

### ViewModel Layer (`ViewModels/TerminalViewModel.swift`)

- Add `processExitCode: Int?` property
- On `.processExited(code)`: set `processExitCode = code`
- On `.restart`: set `processExitCode = nil`

### Session List

- Add `processExited: Bool` to `SessionData` model (maps to `process_exited` from API)
- Show red "EXITED" badge on `SessionCardView` when `processExited` is true

### Terminal View (`Views/TerminalContainerView.swift`)

- When `processExitCode != nil`, write ANSI message into terminal: `[Process exited (code X). Tap session in list to restart.]`
- Status bar shows "process exited (code X)" when applicable

### Restart Flow

Same as web: user navigates back → taps session → viewer joins → relay auto-sends TypeRestart → CLI restarts process. No iOS-specific restart logic needed.
