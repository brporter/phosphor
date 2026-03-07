# API Keys for Daemon Authentication — Design

## Goal

Allow the phosphor daemon to authenticate to the relay using long-lived API keys instead of user OIDC tokens. Any authenticated user can generate an API key via the SPA. Keys are HS256-signed JWTs that carry the generating user's identity. Revocation is file-based.

## API Key Format

HS256-signed JWT with claims:

- `sub` — generating user's identity (e.g. `user@example.com`)
- `iss` — `"phosphor"`
- `jti` — unique key ID (nanoid), used for revocation and display
- `iat` — issued-at timestamp
- No `exp` — keys don't expire; revocation is explicit

Keys are prefixed with `phk:` when transmitted (e.g. `Token: "phk:eyJhbG..."`) to distinguish them from OIDC tokens.

## Relay Changes

### Signing Secret

- `API_KEY_SECRET` env var (required). If not set, relay generates a random 32-byte secret on startup and logs a warning that keys won't survive restarts.
- In dev mode (`DEV_MODE=1`), any `phk:` prefixed token is accepted without signature validation.

### New Endpoint

`POST /api/auth/api-key` — requires bearer token (OIDC-authenticated user).

- Signs a JWT with `API_KEY_SECRET`, returns `{ "api_key": "phk:<jwt>", "key_id": "<jti>" }`.
- Relay does NOT store the key.

### Auth Path

`verifyToken` gains a new branch: when the token starts with `phk:`, strip the prefix and validate the JWT signature. If valid and `jti` is not revoked, return the identity from the JWT's `sub` claim. The `sub` claim uses the format `provider:identity` (e.g. `microsoft:user@example.com`), which is split to produce the provider and sub values.

### Revocation Blocklist

- `API_KEY_REVOCATION_FILE` env var, default `/etc/phosphor/revoked-keys.txt`.
- One `jti` per line. Blank/whitespace lines skipped.
- Loaded on startup. Watched via fsnotify for live reloads.
- Missing file is not an error — no keys revoked.
- `APIKeyBlocklist` struct in `internal/relay/` with `IsRevoked(jti string) bool`.

## Daemon Changes

### Config

Add `api_key` field to `/etc/phosphor/daemon.json`:

```json
{
  "relay": "wss://phosphor.betaporter.dev",
  "api_key": "phk:eyJhbG...",
  "mappings": [...]
}
```

### Startup

Replace `LoadTokenCache()` in `cmd/phosphor/daemon.go` with reading `cfg.ApiKey`. If empty, error with guidance to generate one via the SPA and set it with `phosphor daemon set-key`.

### New CLI Command

`phosphor daemon set-key <key>` — writes the API key to the daemon config file.

### Auth Failure Retry Limit

The daemon's reconnect loop distinguishes auth failures (server returns `auth_failed` or `invalid_token` error codes) from transient failures (network errors, connection drops). Auth failures are retried at most 3 times, then the mapping is stopped with a clear log message. Transient failures continue with the existing exponential backoff.

## SPA Changes

### New Settings Page

- New route `/settings`, protected by auth.
- "settings" link added to header (next to user email, visible when logged in).
- Contains a "Generate API Key" section with a button.
- On success, displays the API key and key ID in a copyable format with a warning that it won't be shown again.

### New Files

- `web/src/components/SettingsPage.tsx`
- `web/src/lib/api.ts` — add `generateApiKey()` function

### Routing

Add `/settings` as a protected route.

## Error Handling

- **Invalid/revoked key at connection time:** relay returns `auth_failed`. Daemon retries up to 3 times, then stops the mapping with a clear log message.
- **Missing blocklist file:** not an error, no keys revoked. fsnotify picks up the file if it appears later.
- **Missing `API_KEY_SECRET`:** relay auto-generates a random secret, logs a warning.
- **Dev mode:** any `phk:` token accepted without validation, identity falls back to `("dev", "anonymous")`.
