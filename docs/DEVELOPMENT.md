# Development Setup

## Prerequisites

- Go 1.24+
- Node.js 20+ and npm

## Environment

Copy the template and adjust as needed:

```sh
cp .env-template .env
```

The `.env` file is loaded automatically by the relay at startup. The template has `DEV_MODE=1` set, which bypasses OIDC authentication so you can develop without configuring identity providers.

See `.env-template` for all available variables.

## Running Locally

Local development requires two terminals:

**Terminal 1 — Relay server:**

```sh
go run ./cmd/relay
```

Starts the relay on `:8080` (or whatever `ADDR` is set to in `.env`).

**Terminal 2 — Web frontend:**

```sh
cd web && npm ci && npm run dev
```

Starts the Vite dev server on `:3000`, proxying `/ws` and `/api` to the relay on `:8080`.

**CLI:**

```sh
go run ./cmd/phosphor --relay ws://localhost:8080 -- bash
```

## Building

```sh
# All (web, CLI, relay)
go build -o bin/phosphor ./cmd/phosphor
go build -o bin/relay ./cmd/relay
cd web && npm ci && npm run build

# CLI with production relay URL baked in
go build -ldflags '-s -w -X github.com/brporter/phosphor/internal/cli.DefaultRelayURL=wss://phosphor.betaporter.dev' -o bin/phosphor ./cmd/phosphor
```

## Testing

```sh
go test ./... -count=1
cd web && npm test
```

## Clean

```sh
rm -rf bin/ web/dist/ web/node_modules/
```
