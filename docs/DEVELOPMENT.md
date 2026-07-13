# Development Setup

## Prerequisites

- Go 1.25+
- Node.js 20+ and npm
- Docker (for local Postgres)
- An SSH daemon on any machine you want to reach (OpenSSH)

## Environment

Copy the template and adjust as needed:

```sh
cp .env-template .env
```

The `.env` file is loaded automatically by the relay at startup. The template has `DEV_MODE=1` set, which bypasses OIDC authentication so you can develop without configuring identity providers. It also sets `DATABASE_URL` to the local Postgres below.

See `.env-template` for all available variables.

## Running Locally

Local development needs Postgres plus two terminals.

**Postgres:**

```sh
docker compose up -d postgres
```

**Terminal 1 — Relay server (+ SSH gateway):**

```sh
go run ./cmd/relay
```

Starts the relay on `:8080` and the SSH gateway on `:2222`. Migrations apply automatically. On first start it generates and persists an SSH host key at `SSH_HOST_KEY_FILE` (`./ssh_host_key` in dev).

**Terminal 2 — Web frontend:**

```sh
make wasm            # build the browser SSH client into web/public/
cd web && npm ci && npm run dev
```

Starts the Vite dev server on `:3000`, proxying `/ws` and `/api` to the relay on `:8080`. Re-run `make wasm` after changing anything under `cmd/webssh/` or `internal/webssh/`.

**Enroll and tunnel a machine:**

```sh
go run ./cmd/phosphor enroll --relay http://localhost:8080
go run ./cmd/phosphor tunnel
```

Then open the web app, sign in, and connect to the machine.

### Testing the tunnel without the browser

For quick SSH-path testing, set `DEV_MODE=1` and the debug listener so a normal `ssh` client can reach a machine's tunnel:

```sh
SSH_DEBUG_LISTEN=127.0.0.1:2200 SSH_DEBUG_MACHINE=<machine-id> go run ./cmd/relay
# in another terminal, with `phosphor tunnel` running:
ssh -p 2200 <user>@localhost
```

## Building

```sh
make wasm                                  # browser SSH client -> web/public/
go build -o bin/phosphor ./cmd/phosphor
go build -o bin/relay ./cmd/relay
cd web && npm ci && npm run build

# CLI with production relay URL baked in
go build -ldflags '-s -w -X github.com/brporter/phosphor/internal/cli.DefaultRelayURL=https://phosphor.betaporter.dev' -o bin/phosphor ./cmd/phosphor
```

## Testing

```sh
# Go tests. Set TEST_DATABASE_URL to include the store package's Postgres tests.
TEST_DATABASE_URL=postgres://phosphor:phosphor@localhost:5432/phosphor go test ./... -count=1
cd web && npm test
```

## Clean

```sh
rm -rf bin/ web/dist/ web/node_modules/ web/public/phosphor-ssh.wasm web/public/wasm_exec.js
docker compose down -v   # also drops the local Postgres volume
```
