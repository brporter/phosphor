# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Phosphor is a real-time terminal sharing tool. A CLI streams a local terminal session over WebSockets to a relay server, which broadcasts to browser viewers via a React SPA. Supports OIDC authentication (Microsoft, Google, Apple).

## Build & Development Commands

All top-level commands are in the `Makefile`:

| Command | Description |
|---|---|
| `make` | Build everything (web → CLI → relay) |
| `make build-web` | `cd web && npm ci && npm run build` |
| `make build-cli` | `go build -o bin/phosphor ./cmd/phosphor` |
| `make build-relay` | `go build -o bin/relay ./cmd/relay` |
| `make dev-relay` | `go run ./cmd/relay` (relay on :8080) |
| `make dev-web` | Vite dev server on :3000, proxies `/ws` and `/api` to :8080 |
| `make clean` | Remove `bin/`, `web/dist/`, `web/node_modules/` |

**Local dev requires two terminals:** `make dev-relay` and `make dev-web`.

Set `DEV_MODE=1` when running the relay to bypass OIDC authentication.

No test or lint commands exist yet.

## Architecture

### Three Runtime Components

1. **`phosphor` CLI** (`cmd/phosphor/`, `internal/cli/`) — runs on the user's machine, wraps a subprocess in a PTY (or reads from stdin in pipe mode), connects to relay via WebSocket at `/ws/cli`. Auth via relay-mediated browser flow (default) or device code flow (`--device-code` for Microsoft/Google). Tokens cached at `~/.config/phosphor/tokens.json`.

2. **`relay` server** (`cmd/relay/`, `internal/relay/`) — stateful Go HTTP server using `net/http` (no framework). Routes: `/ws/cli` (CLI connections), `/ws/view/{id}` (browser viewers), `/api/sessions` (REST list), `/api/auth/*` (relay-mediated OIDC flow), `/health`. The Hub holds an in-memory map of Sessions; each Session has one CLI conn and up to 10 viewer conns.

3. **Web SPA** (`web/`) — React 19 + Vite 6 + TypeScript 5.7. Terminal rendered with xterm.js (WebGL addon). Auth via relay-mediated flow (no client-side OIDC library). Served by the relay as static files from `web/dist/`.

### Data Flow

CLI stdout → relay → broadcast to all viewers. Viewer keystrokes → relay → CLI stdin (PTY mode only).

### Binary Wire Protocol

Defined in `internal/protocol/` (Go) and `web/src/lib/protocol.ts` (TypeScript) — **kept manually in sync**. Format: 1-byte type prefix + payload (raw bytes for Stdout/Stdin, JSON for control messages like Hello/Welcome/Join/Resize/Error).

## Key Conventions

- **Platform-specific Go files** use build tags: `pty_unix.go` (`//go:build !windows`) and `pty_windows.go` (`//go:build windows`)
- **WebSocket auth** is in-protocol (Hello/Join messages carry the token), not via HTTP headers. REST endpoints use `Authorization: Bearer` header.
- **Config**: relay uses env vars (`ADDR`, `BASE_URL`, `DEV_MODE`, `MICROSOFT_CLIENT_ID`, `GOOGLE_CLIENT_ID`, `APPLE_CLIENT_ID`, etc.). Frontend requires no provider-specific env vars — all OIDC config is server-side.
- **Frontend organization**: `auth/` (OIDC context/hooks), `components/` (React components), `hooks/` (useWebSocket, useSessions), `lib/` (protocol.ts, api.ts)
- **Styling**: raw CSS with custom properties, dark terminal aesthetic (green-on-black, Fira Code, scanline overlay). No CSS framework.
- **IDs**: session and viewer IDs generated with nanoid
- **Deployment**: multi-stage Docker build (node → go → distroless), Azure Container Apps via Bicep (`deploy/`)
