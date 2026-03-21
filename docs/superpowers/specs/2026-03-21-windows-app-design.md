# Phosphor Windows App — Design Specification

## Overview

A native Windows viewer app for Phosphor built with C# / .NET 10 / WinUI3 / NativeAOT. Mirrors the functionality of the iOS app and web SPA: browse active terminal sessions, view/interact with terminals in real time, upload files, and authenticate via OIDC. Distributed via Microsoft Store and standalone MSIX installer.

**Target:** Windows 10 1809+ (build 17763)

## Scope

**In scope (viewer-only):**
- Session list with polling, destroy
- Terminal viewing with OpenConsole renderer
- Interactive input (PTY mode), view-only (pipe mode)
- File upload (picker + drag-and-drop)
- End-to-end encryption (AES-256-GCM)
- Relay-mediated OIDC auth (Microsoft, Google, Apple)
- API key generation (settings)

**Out of scope:**
- CLI streaming (creating sessions) — that remains the Go `phosphor` binary
- Terminal multiplexing or tabs (one session per window)

## Technology Stack

| Component | Choice |
|-----------|--------|
| Language | C# 14 |
| Runtime | .NET 10 LTS with NativeAOT |
| UI Framework | WinUI 3 (Windows App SDK) |
| Terminal Renderer | Microsoft.Terminal.Control (OpenConsole) |
| Auth UI | WebView2 (for OIDC popup) |
| WebSocket | System.Net.WebSockets.ClientWebSocket |
| Crypto | System.Security.Cryptography (AesGcm, PBKDF2) |
| JSON | System.Text.Json with source generators (NativeAOT compatible) |
| MVVM | CommunityToolkit.Mvvm (source generators) |
| Packaging | MSIX (Store + sideload) |

**Fallback:** If `Microsoft.Terminal.Control` proves too tightly coupled to Windows Terminal internals, the terminal pane falls back to a WebView2 control hosting xterm.js (the same renderer used by the web SPA). The rest of the app remains native WinUI3.

## Project Structure

```
windows/
├── Phosphor.sln
├── Phosphor/                        # Main WinUI3 app project
│   ├── Phosphor.csproj              # .NET 10, WinUI3, NativeAOT
│   ├── App.xaml / App.xaml.cs       # App entry, resource dictionaries
│   ├── MainWindow.xaml/.cs          # Shell window (NavigationView)
│   │
│   ├── Models/
│   │   ├── SessionData.cs           # Session list model
│   │   ├── AuthUser.cs              # JWT-parsed user info
│   │   └── FileTransfer.cs          # Upload tracking state
│   │
│   ├── ViewModels/
│   │   ├── AuthViewModel.cs         # Login/logout, token management
│   │   ├── SessionListViewModel.cs  # Session polling, destroy
│   │   └── TerminalViewModel.cs     # WebSocket, protocol, file upload
│   │
│   ├── Views/
│   │   ├── LoginPage.xaml/.cs       # Provider buttons, relay URL config
│   │   ├── SessionListPage.xaml/.cs # Session cards, polling
│   │   ├── TerminalPage.xaml/.cs    # Terminal + status bar + upload
│   │   └── SettingsPage.xaml/.cs    # User info, API keys, logout
│   │
│   ├── Services/
│   │   ├── WebSocketService.cs      # WebSocket connection + receive loop
│   │   ├── AuthService.cs           # Relay-mediated OIDC via WebView2
│   │   ├── ApiClient.cs             # REST calls (/api/sessions, etc.)
│   │   ├── CredentialStore.cs       # Windows Credential Manager storage
│   │   └── ProtocolCodec.cs         # Binary wire protocol (1-byte prefix)
│   │
│   ├── Controls/
│   │   └── TerminalControl.xaml/.cs # Wraps OpenConsole renderer
│   │
│   ├── Helpers/
│   │   └── CryptoHelper.cs          # AES-256-GCM, PBKDF2 key derivation
│   │
│   └── Themes/
│       └── PhosphorTheme.xaml       # Mica + green accent resource dictionary
│
└── Phosphor.Terminal/               # Separate project for OpenConsole interop
    ├── Phosphor.Terminal.csproj
    └── TerminalRenderer.cs          # P/Invoke wrapper around OpenConsole
```

## Architecture

### Pattern: MVVM

- **Models:** Plain C# records/classes for data (SessionData, AuthUser, FileTransfer)
- **ViewModels:** `ObservableObject` subclasses (CommunityToolkit.Mvvm) with `RelayCommand` and `ObservableProperty` source generators. Each page has a corresponding ViewModel.
- **Views:** XAML pages with `x:Bind` to ViewModels. No code-behind logic beyond navigation wiring.
- **Services:** Stateless or singleton services injected into ViewModels. Handle I/O (WebSocket, REST, auth, credential storage).

### Navigation

`MainWindow` hosts a `NavigationView` (left-compact mode):
- **Sessions** (home icon) → `SessionListPage`
- **Settings** (gear icon, positioned at bottom) → `SettingsPage`

Clicking a session card navigates to `TerminalPage` with back button in title bar. Page transitions use WinUI3 built-in animations (drill-in for terminal, slide for lateral nav).

Custom `AppTitleBar` with Phosphor logo, using `ExtendsContentIntoTitleBar`.

### Window Behavior

- Remembers size and position across launches (stored in `ApplicationData.Current.LocalSettings`)
- Minimum size: 600×400
- Default size: 1024×768

## Authentication

### Flow (relay-mediated OIDC)

1. User selects provider (Microsoft, Google, Apple) on `LoginPage`. Available providers fetched from `GET /api/auth/config` so unavailable providers are hidden.
2. `AuthService` POSTs to `/api/auth/login` with `{provider, source: "mobile"}` — reuses the same source as the iOS app, which causes the relay to redirect to the `phosphor://` custom URI scheme after OIDC completes
3. Relay returns `{session_id, auth_url}`
4. App opens a WebView2 dialog (new `Window` with `WebView2` control) navigated to `auth_url`
5. WebView2 monitors navigation via `NavigationStarting` event — when the relay redirects to `phosphor://auth/callback?session={id}`, the dialog intercepts the navigation (suppresses it) and closes. This is the same `phosphor://` redirect the relay already implements for iOS (`handler_auth.go:286`).
6. App polls `/api/auth/poll?session={id}` until token is returned
7. JWT parsed into `AuthUser` model (subject, email, issuer, expiry)
8. Token stored in Windows Credential Manager
9. App navigates to `SessionListPage`

### Token Management

- **Storage:** Windows Credential Manager via `Windows.Security.Credentials.PasswordVault`
  - Target name: `phosphor/{relay_host}`
  - Credential password field contains JSON: `{id_token, relay_url}`
- **On app launch:** Load token from Credential Manager, validate `exp` claim
  - Expired → show `LoginPage`
  - Valid → navigate to `SessionListPage`
- **Logout:** Clear Credential Manager entry, navigate to `LoginPage`

### Relay URL Configuration

- Stored in `ApplicationData.Current.LocalSettings`
- Editable on `LoginPage` and `SettingsPage`
- Defaults to production relay URL

## Session List

### SessionListPage

- `ListView` with custom `DataTemplate` for session cards
- Each card displays: hostname, command, mode badge (PTY/Pipe), viewer count, session ID, status badges (ready, exited, encrypted)
- Destroy session: right-click context menu or swipe → `ContentDialog` confirmation → `DELETE /api/sessions/{id}`
- Pull-to-refresh via `RefreshContainer`
- Empty state with quick-start instructions (how to run the CLI)

### SessionListViewModel

- Polls `GET /api/sessions` every 5 seconds via `PeriodicTimer`
- `ObservableCollection<SessionData>` bound to ListView
- Clicking a card navigates to `TerminalPage` with session ID parameter

### Session Card Styling

- Semi-transparent card surfaces (`rgba(255,255,255,0.04)`) with subtle borders
- Pill-shaped badges with translucent accent backgrounds:
  - Green: PTY mode, connected
  - Cyan: Pipe mode, encrypted
  - Amber: Lazy/connecting
  - Red: Exited, error

## Terminal Viewer

### Terminal Rendering

**Primary: Microsoft.Terminal.Control**

The `TerminalControl` wrapper in `Controls/` bridges the OpenConsole renderer with `TerminalViewModel`:

- **Output path:** WebSocket receives `Stdout` (0x01) bytes → feeds into `TermControl`'s input stream
- **Input path:** `TermControl` raises character/key events → `TerminalViewModel` encodes as `Stdin` (0x02) → sends over WebSocket
- **Resize:** `TermControl` reports dimension changes → `TerminalViewModel` sends `Resize` (0x03) message
- **Mode awareness:** In pipe mode, keyboard input events are suppressed (view-only)

**Terminal theme:**
- Background: `#050808`
- Foreground: `#b0b0b0`
- Cursor: `#00ff41` (blinking bar)
- Font: Cascadia Code (bundled with Terminal control) or Fira Code
- Full CRT aesthetic inside the terminal pane (scanline overlay optional)

**Fallback: WebView2 + xterm.js**

If `Microsoft.Terminal.Control` proves infeasible to embed:
- Terminal pane becomes a `WebView2` control loading a local HTML page
- HTML page initializes xterm.js with the same theme/config as the web SPA
- Communication via `WebView2.CoreWebView2.PostWebMessageAsString` / `WebMessageReceived`
- All surrounding UI (nav, status bar, cards, dialogs) remains native WinUI3

### TerminalPage Layout

- Status bar (top): connection state indicator, command name, viewer count, encryption badge
- Terminal pane (center, fills remaining space): `TerminalControl`
- Upload overlay (bottom, shown during transfers): progress bars per file
- Drag-drop overlay: "Drop file to upload" shown on drag enter (PTY mode only)
- Process exit UI: exit code display + restart button (when `ProcessExited` received)

## WebSocket & Binary Protocol

### ProtocolCodec

Encodes/decodes the Phosphor binary wire protocol. Format: `[1-byte type][payload]`

| Type | Hex | Payload | Direction | Viewer Handles? |
|------|-----|---------|-----------|-----------------|
| Stdout | 0x01 | Raw bytes | Server → App | Yes — feed to terminal |
| Stdin | 0x02 | Raw bytes | App → Server | Yes — send on keypress (PTY only) |
| Resize | 0x03 | JSON `{cols, rows}` | Bidirectional | Yes — send on terminal resize |
| Hello | 0x10 | JSON (see protocol source) | CLI → Server | No — CLI only |
| Welcome | 0x11 | JSON (see protocol source) | Server → CLI | No — CLI only |
| Join | 0x12 | JSON `{token, session_id}` | App → Server | Yes — send on connect |
| Joined | 0x13 | JSON `{mode, cols, rows, command, encrypted, encryption_salt}` | Server → App | Yes — initialize session |
| Reconnect | 0x14 | JSON `{status}` | Server → App | Yes — update status bar |
| End | 0x15 | None | Either | Yes — send on clean disconnect, handle on receive |
| Error | 0x16 | JSON `{code, message}` | Server → App | Yes — show error, may close |
| ProcessExited | 0x17 | JSON `{exit_code}` | Server → App | Yes — show exit UI |
| Restart | 0x18 | None | App → Server | Yes — send on restart button |
| ViewerCount | 0x20 | JSON `{count}` | Server → App | Yes — update status bar |
| Mode | 0x21 | JSON `{mode}` | Server → App | Yes — update mode (may toggle input) |
| SpawnRequest | 0x22 | None | Server → CLI | No — CLI only |
| SpawnComplete | 0x23 | JSON `{cols, rows}` | CLI → Server | No — CLI only |
| Ping | 0x30 | None | Either | Yes — respond with Pong |
| Pong | 0x31 | None | Either | Yes — receive keepalive response |
| FileStart | 0x40 | JSON `{id, name, size}` | App → Server | Yes — send to initiate upload |
| FileChunk | 0x41 | Raw `[8-byte ID][data]` | App → Server | Yes — send file data |
| FileEnd | 0x42 | JSON `{id, sha256}` | App → Server | Yes — send upload completion |
| FileAck | 0x43 | JSON `{id, status, error?, bytes_written?}` | Server → App | Yes — update transfer progress |

JSON serialization uses `System.Text.Json` source generators (`JsonSerializerContext`) for NativeAOT compatibility — no reflection.

### WebSocketService

1. Opens `ClientWebSocket` to `wss://relay/ws/view/{sessionId}` with subprotocol `phosphor`
2. Sends `Join` message with auth token and session ID
3. Background receive loop (`Task.Run`):
   - Reads binary WebSocket frames
   - Decodes via `ProtocolCodec`
   - Dispatches by message type to `TerminalViewModel` callbacks
4. Send methods: `SendStdin(byte[])`, `SendResize(int cols, int rows)`, `SendRestart()`, `SendFileStart/Chunk/End(...)`
5. Ping keepalive: `PeriodicTimer` sends `Ping` (0x30) every 30 seconds
6. On clean disconnect (navigating away from `TerminalPage`), sends `End` (0x15) before closing the WebSocket
7. `CancellationToken` wired to page navigation — triggers clean disconnect sequence

## File Upload

### Trigger

- **Button:** Upload button in `TerminalPage` status bar (hidden in pipe mode)
- **Drag-and-drop:** `TerminalPage` registers `DragOver`/`Drop` handlers. Shows overlay on drag enter.

### Flow

1. User picks file(s) via `FileOpenPicker` or drops files
2. For each file, generate 8-char base62 transfer ID
3. Send `FileStart` (0x40) with `{id, name, size}`
4. Wait for `FileAck` (0x43) `status: "accepted"` (30-second timeout)
5. Read file in 32KB chunks, send each as `FileChunk` (0x41) with `[8-byte ID][chunk data]`
6. During streaming, handle intermediate `FileAck` messages with `status: "progress"` and `bytes_written` — these drive the progress bar updates
7. Compute SHA256 incrementally via `System.Security.Cryptography.IncrementalHash`
8. Send `FileEnd` (0x42) with `{id, sha256}`
9. Wait for final `FileAck` `status: "complete"`

### Progress Tracking

- `FileTransfer` model: id, filename, size, bytesWritten, status, error
- `ObservableCollection<FileTransfer>` displayed as overlay on `TerminalPage`
- `ProgressBar` per active transfer
- Completed transfers auto-removed after 10 seconds
- Errors displayed inline

## Encryption

### Key Derivation

- Triggered when `Joined.Encrypted == true`
- User prompted with `ContentDialog` for passphrase
- PBKDF2-SHA256: 100,000 iterations, salt from `Joined.EncryptionSalt` (base64-decoded)
- Derives 256-bit key for AES-256-GCM
- Uses `System.Security.Cryptography.Rfc2898DeriveBytes`

### Encrypt / Decrypt

- AES-256-GCM via `System.Security.Cryptography.AesGcm`
- Encryption (Stdin): generate 12-byte random nonce, encrypt payload, send `[nonce][ciphertext][tag]`
- Decryption (Stdout): extract nonce (first 12 bytes), decrypt remainder
- NativeAOT compatible

### Key Lifecycle

- Derived key held in memory only (in `TerminalViewModel`)
- Never persisted to disk
- Cleared on disconnect or navigation away from `TerminalPage`

### Buffering

- If encrypted `Stdout` arrives before passphrase entry, buffer messages
- On successful key derivation, flush buffer through decryption → terminal
- On decryption failure (wrong passphrase): clear key, show error, re-prompt
- User cancels dialog: disconnect, navigate back to session list

## UI Theme

### Mica + Green Accent

- **Window backdrop:** Mica material (Windows 11), fallback to solid dark `#1a1a2e` on Windows 10
- **Accent color:** `#00ff41` (Phosphor green) — selected nav items, active badges, buttons, focus rings
- **Typography:** Segoe UI Variable for all app chrome; Cascadia Code / Fira Code for terminal pane only
- **Card surfaces:** Semi-transparent `rgba(255,255,255,0.04)` with subtle `rgba(255,255,255,0.06)` borders
- **Badges:** Pill-shaped with translucent accent backgrounds
  - Green (`#00ff41`): PTY, connected
  - Cyan (`#00e5ff`): Pipe, encrypted
  - Amber (`#ffb000`): Warnings, lazy
  - Red (`#ff3333`): Errors, exited
- **Terminal pane:** Full CRT aesthetic — `#050808` background, `#00ff41` cursor, green-on-black

### Resource Dictionary (PhosphorTheme.xaml)

Overrides WinUI3 dark theme color ramps:
- `SystemAccentColor` → `#00ff41`
- Custom brush resources for card backgrounds, badge variants, status indicators
- Font family resources for terminal vs chrome contexts

## Distribution

### MSIX Packaging

- Single MSIX package for both Microsoft Store and standalone sideloading
- NativeAOT self-contained — no .NET runtime dependency for end users
- Code signing required for Store submission and SmartScreen trust

### Microsoft Store

- Standard Store submission via Partner Center
- Auto-updates managed by Store

### Standalone Installer

- MSIX sideload package downloadable from GitHub Releases or project website
- Requires developer mode or certificate trust for sideloading
- Alternative: MSIX bundle with embedded certificate for enterprise deployment

## Dependencies

| Package | Purpose |
|---------|---------|
| Microsoft.WindowsAppSDK | WinUI 3 framework |
| Microsoft.Windows.SDK.BuildTools | Windows SDK projections |
| CommunityToolkit.Mvvm | MVVM source generators |
| Microsoft.Web.WebView2 | Auth popup + terminal fallback |
| Microsoft.Terminal.Control | OpenConsole terminal renderer |

All crypto, WebSocket, and JSON functionality uses built-in .NET 10 APIs — no additional NuGet packages for those.

## NativeAOT Considerations

- All JSON serialization via source-generated `JsonSerializerContext` (no reflection)
- CommunityToolkit.Mvvm source generators are NativeAOT compatible
- WinUI 3 + NativeAOT supported since .NET 8 / Windows App SDK 1.4+, improved in .NET 10
- XAML trimming may require `rd.xml` directives for custom controls
- `Microsoft.Terminal.Control` interop may need explicit `DynamicDependency` attributes
- Test NativeAOT publish early in development to catch trimming issues
