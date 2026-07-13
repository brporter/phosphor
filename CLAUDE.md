# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Phosphor gives browser-based SSH access to your machines, built on reverse SSH tunnels. A CLI (`phosphor tunnel`) exposes a machine's local `sshd` to the Phosphor relay over a reverse SSH tunnel. Users sign in to the React web app, pick a machine, and a WASM-compiled SSH client in the browser connects **through** the relay to that machine's sshd. The SSH handshake is end-to-end between the browser and the host, so the relay only ever pipes ciphertext — it never sees terminal contents. Supports OIDC authentication (Microsoft, Google, Apple).

## Build & Development Commands

See `docs/DEVELOPMENT.md` for full setup. Quick reference:

| Command | Description |
|---|---|
| `docker compose up -d postgres` | Start local Postgres (required by the relay) |
| `go run ./cmd/relay` | Start relay + SSH gateway (reads `.env` automatically) |
| `make wasm` | Build the browser SSH client to `web/public/phosphor-ssh.wasm` |
| `cd web && npm run dev` | Vite dev server on :3000, proxies `/ws` and `/api` to :8080 |
| `go run ./cmd/phosphor enroll --relay http://localhost:8080` | Enroll this machine |
| `go run ./cmd/phosphor tunnel` | Maintain the reverse tunnel |
| `go build -o bin/relay ./cmd/relay` | Build relay binary |
| `go test ./... -count=1` | Run Go tests (set `TEST_DATABASE_URL` to include store tests) |

**Local dev requires Postgres + two terminals:** `go run ./cmd/relay` and `cd web && npm run dev`. Copy `.env-template` to `.env`; the template has `DEV_MODE=1`, which bypasses OIDC.

## Architecture

### Runtime Components

1. **`phosphor` CLI** (`cmd/phosphor/`, `internal/cli/`) — runs on the machine you want to reach.
   - `phosphor enroll` authenticates (relay-mediated browser flow or `--api-key`), generates an ed25519 **machine key** (`~/.config/phosphor/machine_key`), registers it via `POST /api/machines`, and pins the gateway's SSH endpoint + host key (fetched over TLS from `/api/ssh-info`) into `~/.config/phosphor/machine.json`.
   - `phosphor tunnel` dials the relay's SSH gateway with the machine key, requests a `tcpip-forward`, and bridges each forwarded channel to the local sshd (`127.0.0.1:22` by default). Auto-reconnects with jittered backoff.

2. **`relay` server** (`cmd/relay/`, `internal/relay/`, `internal/sshgate/`) — a Go HTTP server (`net/http`, no framework) plus a native `x/crypto/ssh` gateway.
   - **HTTP routes**: `/ws/ssh/{machineID}` (browser SSH bridge), `/api/machines` (CRUD), `/api/ssh-info`, `/api/auth/*` (OIDC), `/health`, static SPA.
   - **SSH gateway** (`internal/sshgate/`) listens on `SSH_ADDR` (`:2222`), authenticates machines by their enrolled key fingerprint (`PublicKeyCallback`), and tracks live tunnels in an in-memory `Registry`. `Registry.Dial(machineID)` opens a `forwarded-tcpip` channel down the tunnel — one tunnel serves many concurrent browser sessions.
   - **WS bridge** (`handler_ws_ssh.go`): authenticates the browser (JWT + tenant→machine ownership) via a JSON `{token}` prelude, then pipes raw bytes between the WebSocket and `Registry.Dial`.

3. **Web SPA** (`web/`) — React 19 + Vite 6 + TypeScript 5.7. Machine list + xterm.js terminal. The browser SSH client is Go's `x/crypto/ssh` compiled to `GOOS=js GOARCH=wasm` (`cmd/webssh/`, `internal/webssh/`), exposing a `phosphorSSH` global; the `useSSH` hook drives it. Served by the relay as static files from `web/dist/` (with `phosphor-ssh.wasm` + `wasm_exec.js`).

### Data Model (Postgres, `internal/store/`)

`tenants`, `users` (federated identity: provider+subject+email), `machines` (name, fingerprint, tenant), `api_keys` (revocation). First login auto-creates a personal tenant (`GetOrCreateUser`). pgx v5 pool + embedded `golang-migrate` migrations auto-applied at startup.

### Data Flow

Browser WASM SSH client ⇄ (WebSocket) ⇄ relay bridge ⇄ (forwarded-tcpip over the reverse tunnel) ⇄ host sshd. Encryption is end-to-end SSH between browser and host; the relay pipes opaque bytes.

## VERY IMPORTANT: Bash Tool Usage

**NEVER issue compound commands.** Each `git` or shell command must be a separate, single tool call. Do NOT chain commands with `&&`, `;`, or `||`. This causes constant prompt loops.

## Key Conventions

- **WASM build**: the browser SSH client is built with `make wasm` into `web/public/` for dev and `web/dist/` in the Docker image. `wasm_exec.js` comes from `$(go env GOROOT)/lib/wasm/`.
- **Auth**: browser→host SSH uses standard SSH methods (public-key/password/keyboard-interactive) against the host's own sshd. Browser-held keys live in IndexedDB (`web/src/lib/keys.ts`); host-key pins are trust-on-first-use. Machine→gateway auth is SSH public-key. Relay REST uses `Authorization: Bearer`; the WS bridge uses a JSON `{token}` prelude.
- **Config**: relay env vars — `ADDR`, `BASE_URL`, `DEV_MODE`, `DATABASE_URL` (required), `SSH_ADDR`, `SSH_HOST_KEY_FILE`, `SSH_PUBLIC_ADDR`, `API_KEY_SECRET`, `MICROSOFT_CLIENT_ID`/`GOOGLE_CLIENT_ID`/`APPLE_CLIENT_ID` etc. Dev-only: `SSH_DEBUG_LISTEN` + `SSH_DEBUG_MACHINE`.
- **Frontend organization**: `auth/` (OIDC context/hooks), `components/` (MachineList, ConnectView, KeysPage, AuthModal), `hooks/` (useSSH, useMachines), `lib/` (wasm.ts, machines.ts, keys.ts, api.ts).
- **Styling**: raw CSS with custom properties, dark terminal aesthetic (green-on-black, Fira Code, scanline overlay). No CSS framework.
- **IDs**: tenant/user/machine IDs are UUIDs (Postgres); API-key IDs are nanoid.
- **Host prerequisites**: the target machine must run an SSH daemon (OpenSSH on Unix; OpenSSH Server enabled on Windows).
- **Deployment**: multi-stage Docker build (node → go[+wasm] → distroless), pushed to GHCR by CI; a Docker Compose stack (Caddy + relay + Postgres + Watchtower) on a Linux VM pulls and runs it. The SSH gateway port (2222) is exposed directly; Caddy fronts only HTTP/WS. See `deploy/vm/README.md`.
