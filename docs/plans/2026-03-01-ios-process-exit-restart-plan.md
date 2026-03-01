# iOS Process Exit & Restart Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add process exit/restart awareness to the iOS app, matching the web app's behavior.

**Architecture:** Extend the existing protocol/WebSocket/ViewModel layers with two new message types (ProcessExited 0x17, Restart 0x18). The server-side restart-on-join mechanism already works — the iOS app just needs to display exit state and clear it on restart.

**Tech Stack:** Swift, SwiftUI, SwiftTerm, Observation framework

---

### Task 1: Add ProcessExited and Restart message types to protocol

**Files:**
- Modify: `ios/Phosphor/Protocol/MessageType.swift:14` (add after `.error = 0x16`)
- Modify: `ios/Phosphor/Protocol/ProtocolCodec.swift:100` (add after `ModePayload`)

**Step 1: Add message type enum cases**

In `MessageType.swift`, add two cases after `case error = 0x16`:

```swift
    case processExited = 0x17
    case restart       = 0x18
```

**Step 2: Add ProcessExitedPayload struct**

In `ProtocolCodec.swift`, add after the `ModePayload` struct:

```swift
struct ProcessExitedPayload: Codable {
    let exitCode: Int

    enum CodingKeys: String, CodingKey {
        case exitCode = "exit_code"
    }
}
```

**Step 3: Build to verify compilation**

Run: `cd ios && xcodebuild -scheme Phosphor -destination 'generic/platform=iOS' build -quiet 2>&1 | tail -5`
Expected: BUILD SUCCEEDED

**Step 4: Commit**

```
git add ios/Phosphor/Protocol/MessageType.swift ios/Phosphor/Protocol/ProtocolCodec.swift
git commit -m "feat(ios): add ProcessExited and Restart protocol message types"
```

---

### Task 2: Handle ProcessExited and Restart in WebSocket layer

**Files:**
- Modify: `ios/Phosphor/Services/WebSocketManager.swift:4` (add event cases)
- Modify: `ios/Phosphor/Services/WebSocketManager.swift:146` (add message handling before `case .ping`)

**Step 1: Add WebSocketEvent cases**

In `WebSocketManager.swift`, add two cases to the `WebSocketEvent` enum after `case error(String)`:

```swift
    case processExited(Int)
    case restart
```

**Step 2: Handle messages in handleBinaryMessage**

In `WebSocketManager.swift`, add cases in the `switch type` block after the `.error` case (before `.ping`):

```swift
        case .processExited:
            if let info: ProcessExitedPayload = try? ProtocolCodec.decodeJSON(payload) {
                continuation?.yield(.processExited(info.exitCode))
            }
        case .restart:
            continuation?.yield(.restart)
```

**Step 3: Build to verify compilation**

Run: `cd ios && xcodebuild -scheme Phosphor -destination 'generic/platform=iOS' build -quiet 2>&1 | tail -5`
Expected: BUILD SUCCEEDED

**Step 4: Commit**

```
git add ios/Phosphor/Services/WebSocketManager.swift
git commit -m "feat(ios): handle ProcessExited and Restart WebSocket events"
```

---

### Task 3: Track process exit state in TerminalViewModel

**Files:**
- Modify: `ios/Phosphor/ViewModels/TerminalViewModel.swift:17` (add property)
- Modify: `ios/Phosphor/ViewModels/TerminalViewModel.swift:71` (add event handling)

**Step 1: Add processExitCode property**

In `TerminalViewModel.swift`, add after `var viewerCount: Int = 0`:

```swift
    var processExitCode: Int?
```

**Step 2: Add onProcessExited callback**

In `TerminalViewModel.swift`, add after the `onResize` callback:

```swift
    /// Callback invoked when the process exits (writes message to terminal).
    var onProcessExited: ((Int) -> Void)?
```

**Step 3: Handle events in handleEvent**

In `TerminalViewModel.swift`, add cases in the `switch event` block after the `.disconnected` case:

```swift
        case .processExited(let code):
            processExitCode = code
            onProcessExited?(code)

        case .restart:
            processExitCode = nil
```

**Step 4: Build to verify compilation**

Run: `cd ios && xcodebuild -scheme Phosphor -destination 'generic/platform=iOS' build -quiet 2>&1 | tail -5`
Expected: BUILD SUCCEEDED

**Step 5: Commit**

```
git add ios/Phosphor/ViewModels/TerminalViewModel.swift
git commit -m "feat(ios): track process exit state in TerminalViewModel"
```

---

### Task 4: Display process exit state in TerminalContainerView

**Files:**
- Modify: `ios/Phosphor/Views/TerminalContainerView.swift:111` (status bar text)
- Modify: `ios/Phosphor/Views/TerminalContainerView.swift:120` (status color)

**Step 1: Update status bar to show process exit state**

In `TerminalContainerView.swift`, replace the status bar connection state text (line 111-113):

```swift
            Text(viewModel.connectionState.rawValue)
                .font(.system(size: 10, design: .monospaced))
                .foregroundStyle(PhosphorTheme.text.opacity(0.6))
```

with:

```swift
            if let exitCode = viewModel.processExitCode {
                Text("process exited (\(exitCode))")
                    .font(.system(size: 10, design: .monospaced))
                    .foregroundStyle(PhosphorTheme.amber)
            } else {
                Text(viewModel.connectionState.rawValue)
                    .font(.system(size: 10, design: .monospaced))
                    .foregroundStyle(PhosphorTheme.text.opacity(0.6))
            }
```

**Step 2: Update statusColor for process exit state**

In `TerminalContainerView.swift`, update the `statusColor` computed property. Change `case .connected`:

```swift
        case .connected:
            return viewModel.processExitCode != nil ? PhosphorTheme.amber : PhosphorTheme.green
```

**Step 3: Build to verify compilation**

Run: `cd ios && xcodebuild -scheme Phosphor -destination 'generic/platform=iOS' build -quiet 2>&1 | tail -5`
Expected: BUILD SUCCEEDED

**Step 4: Commit**

```
git add ios/Phosphor/Views/TerminalContainerView.swift
git commit -m "feat(ios): show process exit state in terminal status bar"
```

---

### Task 5: Write ANSI exit message to terminal

**Files:**
- Modify: `ios/Phosphor/Views/TerminalContainerView.swift:56` (TerminalRepresentable setup)

The web app writes `\r\n\x1b[1;33m[Process exited (code X). Click session in list to restart.]\x1b[0m\r\n` into xterm.js. We need to do the same via SwiftTerm's `feed` method.

**Step 1: Wire up onProcessExited callback**

In `TerminalContainerView.swift`, the `TerminalRepresentable` is created on line 56. We need to use the `onProcessExited` callback on the viewModel to feed the ANSI message into the terminal. Add this in the `.onAppear` block, after `viewModel.connect(...)`:

```swift
            viewModel.onProcessExited = { code in
                let message = "\r\n\u{1B}[1;33m[Process exited (code \(code)). Tap session in list to restart.]\u{1B}[0m\r\n"
                if let data = message.data(using: .utf8) {
                    viewModel.onStdout?(data)
                }
            }
```

**Step 2: Build to verify compilation**

Run: `cd ios && xcodebuild -scheme Phosphor -destination 'generic/platform=iOS' build -quiet 2>&1 | tail -5`
Expected: BUILD SUCCEEDED

**Step 3: Commit**

```
git add ios/Phosphor/Views/TerminalContainerView.swift
git commit -m "feat(ios): write ANSI exit message to terminal on process exit"
```

---

### Task 6: Add processExited to SessionData and SessionCardView

**Files:**
- Modify: `ios/Phosphor/Models/Session.swift:9` (add field)
- Modify: `ios/Phosphor/Views/SessionCardView.swift:24` (add badge)

**Step 1: Add processExited field to SessionData**

In `Session.swift`, add after `let viewers: Int`:

```swift
    let processExited: Bool

    enum CodingKeys: String, CodingKey {
        case id, mode, cols, rows, command, viewers
        case processExited = "process_exited"
    }
```

**Step 2: Add "EXITED" badge to SessionCardView**

In `SessionCardView.swift`, add after the mode badge `clipShape` (after line 24):

```swift
                // Exited badge
                if session.processExited {
                    Text("EXITED")
                        .font(.system(size: 10, weight: .bold, design: .monospaced))
                        .foregroundStyle(PhosphorTheme.red)
                        .padding(.horizontal, 8)
                        .padding(.vertical, 3)
                        .overlay(
                            RoundedRectangle(cornerRadius: 4)
                                .strokeBorder(PhosphorTheme.red, lineWidth: 1)
                        )
                }
```

**Step 3: Build to verify compilation**

Run: `cd ios && xcodebuild -scheme Phosphor -destination 'generic/platform=iOS' build -quiet 2>&1 | tail -5`
Expected: BUILD SUCCEEDED

**Step 4: Commit**

```
git add ios/Phosphor/Models/Session.swift ios/Phosphor/Views/SessionCardView.swift
git commit -m "feat(ios): show exited badge on session cards"
```
