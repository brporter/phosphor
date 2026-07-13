# Phosphor v.Next: SSH-Tunnel Architecture

## Context

Phosphor today is a terminal-*sharing* tool: the CLI streams a PTY over a custom WebSocket protocol to a relay that broadcasts to browser viewers. Per `designs/ssh-tunnels.md`, this has three problems: session management is awkward (client creates a session users connect to), user mapping is manual (`/etc/phosphor/daemon.json`), and E2E encryption needs a one-off pre-shared passphrase.

v.Next pivots the product to browser-based *remote access* built exclusively on SSH tunnels. The CLI opens a reverse SSH tunnel exposing the host's own `sshd` to the Phosphor backend; the browser runs a WASM SSH client that connects *through* the backend to that sshd. The SSH handshake is browser↔host, so the backend pipes only ciphertext — the E2E guarantee comes free from SSH, with no pre-shared secret.

**Decisions:**
- **Clean replacement** — old WebSocket protocol, viewers/broadcast, pipe mode, passphrase E2E all removed.
- Browser→host auth: **standard SSH methods** (public-key/password/keyboard-interactive). Key management is **fully manual in v1** (generate/import in browser, user copies pubkey to `authorized_keys`); a `phosphor authorize-key` assist is a later phase.
- Backend is a **native Go `x/crypto/ssh` server** — no OpenSSH, no PAM (the original doc's PAM module is superseded; auth is Go callbacks).
- Durable state in **Postgres** (new compose service). Redis dropped entirely.
- **Tenancy**: first login auto-creates a personal tenant; invites/multi-user tenants deferred (schema supports them).
- **Native iOS/Windows apps deleted** in the cleanup milestone (git history preserves them).

## Architecture

```
 Host machine                Phosphor backend (single VM)            Browser
┌──────────────┐  SSH :2222 ┌──────────────────────────────┐  WSS   ┌───────────────┐
│ sshd :22     │◄───────────│ sshgate (x/crypto/ssh server)│◄───────│ React SPA     │
│      ▲       │  reverse   │  + in-memory TunnelRegistry  │ (Caddy)│ xterm.js      │
│ phosphor CLI │  tunnel    │ HTTP: /api, /ws/ssh/{id}     │        │ Go WASM SSH   │
└──────────────┘            │ Postgres: tenants/users/     │        │ client        │
                            │   machines/api_keys          │        └───────────────┘
                            └──────────────────────────────┘
   SSH handshake & encryption: browser ⇄ host sshd (backend pipes ciphertext only)
```

- CLI authenticates to sshgate via **SSH public-key auth with an enrolled ed25519 machine key** (rejected: `phk:` API key in password field — long-lived bearer secret on every connect, no per-machine revocation). It requests a standard `tcpip-forward`; the backend binds nothing, just records the tunnel in the registry.
- Browser WS bridge: authenticate (JWT + user→tenant→machine check), then `registry.Dial(machineID)` opens a `forwarded-tcpip` channel down the tunnel and the backend blindly pipes bytes. SSH channel multiplexing means **one tunnel serves many concurrent browser sessions** (each `Accept()` on the CLI's `client.Listen` dials `127.0.0.1:22` fresh).
- WASM client is **the same `x/crypto/ssh` compiled with `GOOS=js GOARCH=wasm`** (~10–12MB raw, ~3MB brotli, lazily loaded). TinyGo rejected (incomplete reflect/crypto support breaks x/crypto/ssh); no maintained browser JS SSH library exists.
- Direct (non-browser) SSH clients can't reach hosts simply because no public listener maps to tunnels — only the authenticated WS bridge does.

## Data Model (Postgres, new `internal/store` package)

pgx v5 + `golang-migrate` with embedded `iofs` migrations, auto-applied at relay startup (required: distroless image + Watchtower auto-deploys).

- `tenants(id, name, created_at)`
- `users(id, tenant_id FK, provider, subject, email, created_at, UNIQUE(provider, subject))`
- `machines(id, tenant_id FK, name, hostname, fingerprint UNIQUE — ssh.FingerprintSHA256 of machine pubkey, last_seen_at, UNIQUE(tenant_id, name))`
- `api_keys(key_id PK — JWT jti, user_id FK, created_at, revoked_at)` — keeps the existing `phk:` HS256 JWT format from `internal/relay/apikey.go`; DB `revoked_at` check **replaces the file blocklist**.

Online status is *not* persisted — derived from the in-memory TunnelRegistry; `last_seen_at` updated on connect/disconnect.

New deps: `jackc/pgx/v5`, `golang-migrate/migrate/v4`, `google/uuid`. Removed: `redis/go-redis/v9`.

## Component Changes

### CLI (`cmd/phosphor`, `internal/cli`)
- **New**: `internal/cli/enroll.go`, `tunnel.go`, `machinekey.go`.
  - `phosphor enroll [--name] [--api-key phk:...]`: reuse existing `BrowserLogin` (`internal/cli/browser.go`) or API key → generate ed25519 keypair → key at `~/.config/phosphor/machine_key` (0600) → `POST /api/machines {name, hostname, public_key}` → save `machine_id` to `~/.config/phosphor/machine.json`.
  - `phosphor tunnel` (default subcommand): `ssh.Dial` to the relay's SSH port with `User: machineID`, `ssh.PublicKeys(signer)`, host key pinned via `GET /api/ssh-info` (fetched over TLS — no TOFU hole). Then `client.Listen("tcp", "0.0.0.0:22")` (symbolic; emits `tcpip-forward`), accept loop dials `--sshd-addr` (default `127.0.0.1:22`) per channel, bidirectional `io.Copy`. Keepalive `keepalive@openssh.com` every 30s / 15s deadline; reconnect with exponential backoff + jitter (survives Watchtower redeploys).
- **Kept**: `browser.go`, `login.go`, `config.go` (trimmed), token cache, OIDC/device-code auth.
- **Deleted**: `ws.go`, `app.go`, `pty*.go`, `resize_*.go`, `pipe.go`, `file_receiver.go`, `notifier.go` (+tests).
- `internal/daemon/`: keep service install/run scaffolding; delete `Mapping`, `spawn_unix.go`, `spawn_windows.go`. Daemon = the tunnel loop with config `{relay, sshd_addr}`.

### Backend (new `internal/sshgate` + relay rewiring)
- `sshgate/hostkey.go`: ed25519 host key from `SSH_HOST_KEY_FILE` (default `/etc/phosphor/ssh_host_key`), generate-and-persist if missing; compose volume keeps the fingerprint stable.
- `sshgate/server.go`: `net.Listen` on `SSH_ADDR` (`:2222`); `ssh.ServerConfig{MaxAuthTries: 3, PublicKeyCallback}` — callback looks up machine by fingerprint, returns `ssh.Permissions.Extensions{machine-id, tenant-id}`. Reject all client-opened channels; handle `tcpip-forward` (register), `cancel-tcpip-forward`, keepalives (90s inactivity → close); unregister + update `last_seen_at` on close.
- `sshgate/registry.go`: `map[machineID]*Tunnel` (RWMutex); new tunnel **replaces** stale one (zombie handling). `Dial(machineID)` → `conn.OpenChannel("forwarded-tcpip", ...)` echoing the CLI's requested bind addr/port, wrapped as `net.Conn`. Plus `Online`/`OnlineSet` for the API.
- `internal/relay/handler_ws_ssh.go`: `GET /ws/ssh/{machineID}` via `coder/websocket`, subprotocol `phosphor-ssh`. First WS message is JSON `{token}` (in-protocol auth, matches existing convention; keeps token out of URLs/logs) → verify, check tenant→machine → `{ok}` → raw piping via `websocket.NetConn` + two `io.Copy`s against `registry.Dial`. Limits: ~16 bridges/machine, per-user cap, ~30min idle timeout.
- `internal/relay/handler_machines.go`: `GET/POST /api/machines`, `PATCH/DELETE /api/machines/{id}`, `GET /api/ssh-info` (host-key fingerprint + port). Tenancy bootstrap (get-or-create user+tenant) wraps `IdentityFromContext`.
- `cmd/relay/main.go`: drop Redis branch, add `DATABASE_URL` (pgxpool + migrations), start sshgate alongside HTTP, graceful shutdown for both.
- **Dev/M1 test path**: `SSH_DEBUG_LISTEN=127.0.0.1:2200` (honored only with `DEV_MODE`) binds a real TCP listener piped to `registry.Dial(SSH_DEBUG_MACHINE)` so `ssh -p 2200 user@localhost` exercises the tunnel before any WASM exists.
- **Deleted**: `internal/relay/`: `hub.go`, `local_session.go`, `store*.go`, `bus_*.go`, `handler_ws_cli.go`, `handler_ws_viewer.go`, `handler_api.go`, `blocklist.go`, `authsession_redis.go` (+tests). Whole packages: `internal/protocol/`, `internal/crypto/`.

### WASM SSH client (new `cmd/webssh` + `internal/webssh`, `//go:build js && wasm`)
- `wsconn.go`: `net.Conn` over a browser WebSocket via `syscall/js` (channel-backed reads; never block in JS callbacks).
- `client.go`: `ssh.NewClientConn(wsConn, ...)`; `HostKeyCallback` → JS `verifyHostKey(fingerprint)` promise (TOFU + IndexedDB pins in TS); auth methods: `ssh.PublicKeys` (PEM passed in, `ParsePrivateKey[WithPassphrase]`), `ssh.PasswordCallback`, `ssh.KeyboardInteractive` — all prompting via JS; session: `RequestPty("xterm-256color", ...)`, `Shell()`, stdout→JS `onData`, exported `write`/`resize` (`WindowChange`).
- JS surface (`phosphorSSH` global): `connect(opts)→handle`, `generateKeypair(passphrase?)→{privateKeyPem, authorizedKey, fingerprint}` (ed25519).

### Frontend (`web/`)
- `lib/wasm.ts`: lazy `WebAssembly.instantiateStreaming` of `phosphor-ssh.wasm` + `wasm_exec.js` from `web/public/` (no Vite plugin needed; built by `make wasm` + Docker).
- `hooks/useSSH.ts` replaces `useWebSocket.ts`, preserving the shape TerminalView expects (`connected, error, sendStdin, sendResize`) plus `authRequest` state (`password | keyboard-interactive | hostkey | key-passphrase` with `respond()`) driving modals.
- `lib/keys.ts`: IndexedDB for keypairs (optionally passphrase-encrypted PEM) and host-key pins + remembered usernames.
- Components: `MachineList`/`MachineCard` (replace SessionList/Card; name/hostname/online/last-seen), `ConnectView` at `/machine/:id` (xterm.js unchanged + username field + auth modals), `KeysPage` (generate/import/delete, copy `authorized_keys` line), `useMachines` hook (poll `GET /api/machines` 5s). Settings keeps API-key generation (headless enroll).
- **Deleted**: `lib/protocol.ts`, `lib/crypto.ts`, `hooks/useWebSocket.ts`, `useSessions.ts`, SessionList/SessionCard, `e2e/encryption.spec.ts` (+tests).

### Deployment
- `deploy/Dockerfile`: go-build stage adds `GOOS=js GOARCH=wasm go build -o /wasm/phosphor-ssh.wasm ./cmd/webssh` + copies `$(go env GOROOT)/lib/wasm/wasm_exec.js`; final stage layers both into `/web/dist/`. `EXPOSE 8080 2222`.
- `deploy/vm/docker-compose.yml`: drop `redis`; add `postgres:17-alpine` (volume, healthcheck); relay gets `DATABASE_URL`, `SSH_ADDR`, `ports: ["2222:2222"]` (direct exposure; Caddy stays HTTP/WS-only), volume at `/etc/phosphor` for the host key. Open 2222 in the VM's NSG.
- `.env`: add `DATABASE_URL`, `POSTGRES_PASSWORD`, `SSH_ADDR`, `SSH_HOST_KEY_FILE`; remove `REDIS_URL`, `API_KEY_REVOCATION_FILE`, `GRACE_PERIOD`. Root `docker-compose.yml` (dev): redis→postgres.
- Docs: rewrite `CLAUDE.md`, `README.md`, `docs/DEVELOPMENT.md`, `deploy/vm/README.md` (incl. host prerequisites: sshd required; Windows hosts need OpenSSH Server).

## Milestones

**M0 — Data layer**: `internal/store` (pgx, migrations, CRUD, `GetOrCreateUser` bootstrap), `DATABASE_URL` wiring, DB-backed key revocation, empty `GET /api/machines`, dev-compose Postgres.
*Verify*: `go test ./internal/store` against dockerized Postgres; relay boots; `/api/auth/*` unchanged; API key round-trip.

**M1 — SSH gateway + CLI tunnel**: `internal/sshgate`, `POST /api/machines`, `/api/ssh-info`, `phosphor enroll`/`tunnel` with keepalive+reconnect, `SSH_DEBUG_LISTEN`.
*Verify*: enroll + tunnel locally; `ssh -p 2200 $USER@localhost` reaches local sshd through the tunnel; kill relay → CLI reconnects; two concurrent debug sessions over one tunnel.

**M2 — WS bridge + WASM MVP (password auth)**: `handler_ws_ssh.go`, `cmd/webssh`/`internal/webssh`, `wasm.ts`, `useSSH`, MachineList, ConnectView with password + accept-and-pin host key, `make wasm`, Dockerfile stage.
*Verify*: browser → machine list → connect → password login → interactive shell; resize; vim/htop render; backend logs show only byte counts.

**M3 — Key management + auth polish**: WASM `generateKeypair`, `keys.ts`, KeysPage, pubkey auth, keyboard-interactive, host-key mismatch warning, remembered usernames.
*Verify*: generate key in browser → paste into `authorized_keys` → passwordless login; wrong host key blocks with warning.

**M4 — Deletion + cutover**: delete all legacy code listed above **plus `ios/` and `windows/` app directories**; drop Redis dep; slim daemon; compose/NSG/env changes; docs; deploy.
*Verify*: `go build ./...`, `go test ./... -count=1`, `cd web && npm run build && npm test`; full manual E2E on the VM.

**Later**: `phosphor authorize-key` assist, SFTP file transfer, tenant invites, multi-instance tunnel routing.

## Testing

- **Go unit**: registry register/replace/dial; `PublicKeyCallback` vs fake store; machines handlers (httptest); WS-bridge auth prelude.
- **Go integration (in-process)**: sshgate on ephemeral port + trivial fake x/crypto/ssh sshd + CLI tunnel goroutine; assert byte round-trips and N concurrent dials; second test drives the real WS bridge with a `coder/websocket` client.
- **Store**: real Postgres when `TEST_DATABASE_URL` set (CI service), skip otherwise.
- **Frontend**: vitest for `keys.ts`/`useMachines`/auth reducers (mock `phosphorSSH` global); one Playwright E2E with containerized sshd (`linuxserver/openssh-server`) doing password login.
- **Manual E2E**: enroll → tunnel → password login → generate key → pubkey login → kill relay mid-session (browser disconnect prompt, CLI reconnects, browser reconnect works) → Watchtower redeploy and repeat.

## Risks / Notes

1. **WASM size**: ~3MB brotli, lazy-loaded + cache-immutable — acceptable; TinyGo fallback is a research project, don't count on it.
2. **WASM crypto throughput**: no hardware AES; tens of MB/s — fine for terminals, revisit for SFTP.
3. **Security posture shift**: backend can *attempt* SSH connections to enrolled hosts' sshds (can't decrypt, but a compromised backend could brute-force host auth) — mitigate with per-bridge rate limits; document.
4. **Single-instance registry** pins deployment to one relay (current reality anyway).
5. **In-flight browser sessions drop on Watchtower redeploy** (same as today); CLI tunnels auto-reconnect.
6. **Postgres ops**: new backup surface — nightly `pg_dump` cron on the VM is sufficient.
