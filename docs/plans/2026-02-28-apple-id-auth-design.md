# Apple ID Authentication + Relay-Mediated Browser Login

## Summary

Add Apple ID as a third OIDC provider and introduce relay-mediated browser-based authentication for all providers in the CLI. Device code flow remains available for Microsoft/Google via `--device-code` flag.

## Motivation

- Apple does not support the device code flow (RFC 8628)
- Apple's OIDC callback uses `response_mode=form_post` (POST, not GET), which `oidc-client-ts` doesn't handle natively
- A relay-mediated flow solves both constraints and provides a consistent auth experience across all providers

## Architecture

### Relay Auth Endpoints

Four new HTTP endpoints on the relay server:

**`POST /api/auth/login`** — Starts a browser-based login flow
- Body: `{ "provider": "apple" | "microsoft" | "google" }`
- Creates a pending auth session (nanoid key, 5-min TTL) with PKCE code verifier
- Returns `{ "session_id": "...", "auth_url": "https://relay/api/auth/authorize?session=..." }`

**`GET /api/auth/authorize`** — Browser visits this; relay redirects to OIDC provider
- Query: `?session=SESSION_ID`
- Builds the provider's authorization URL with `redirect_uri=BASE_URL/api/auth/callback`, PKCE challenge, and session ID as `state`
- 302 redirect to provider's authorize endpoint

**`GET /api/auth/callback` (+ `POST` for Apple)** — Provider redirects back here
- Receives `code` + `state` (session ID)
- Exchanges auth code for tokens with the provider (using PKCE verifier + client secret)
- Stores the ID token in the pending auth session
- Returns HTML page: "Authentication complete. You can close this tab."

**`GET /api/auth/poll`** — CLI/SPA polls for completion
- Query: `?session=SESSION_ID`
- Returns `{ "status": "pending" }` or `{ "status": "complete", "id_token": "..." }`
- Session deleted after successful retrieval (single-use)

### Auth Session Store

In-memory store in the relay:

```go
type AuthSession struct {
    ID           string
    Provider     string
    CodeVerifier string    // PKCE
    CreatedAt    time.Time
    IDToken      string    // populated after callback
}
```

- Mutex-protected map of `sessionID → AuthSession`
- 5-minute TTL with background cleanup goroutine
- Consumed (deleted) on successful poll

### Apple Client Secret JWT

Apple requires a dynamically generated JWT as the client secret, signed with a P-256 ECDSA private key.

```go
// internal/auth/apple.go
func GenerateAppleClientSecret(teamID, clientID, keyID string, privateKey *ecdsa.PrivateKey) (string, error)
```

- Header: `alg: ES256`, `kid: keyID`
- Claims: `iss: teamID`, `sub: clientID`, `aud: https://appleid.apple.com`, `iat`, `exp` (6 months)

### CLI Changes

- `phosphor login --provider apple|microsoft|google` uses relay-mediated browser flow (default)
- `phosphor login --provider microsoft|google --device-code` uses device code flow (legacy)
- Flow: CLI calls `POST /api/auth/login` on relay, opens browser, polls `/api/auth/poll` every 2s, caches token on completion
- Relay URL sourced from `--relay` flag or default config

### Web SPA Changes

- Apple auth routes through the relay (same `/api/auth/*` endpoints) due to `form_post` constraint
- Microsoft and Google continue using direct `oidc-client-ts` flow
- Apple provider config added to `AuthProvider.tsx` and `AuthCallback.tsx`
- "Sign in with Apple" button added to `Layout.tsx` and `ProtectedRoute.tsx`
- For Apple web login: SPA calls `POST /api/auth/login`, redirects browser to `auth_url`, relay handles callback, SPA polls for token

### ProviderConfig Extension

```go
type ProviderConfig struct {
    Name            string
    Issuer          string
    ClientID        string
    ClientSecret    string
    DeviceAuthURL   string
    // New fields for Apple support:
    TeamID          string            // Apple only
    KeyID           string            // Apple only
    PrivateKey      *ecdsa.PrivateKey // Apple only
}
```

## Environment Variables

### Relay (new)

| Variable | Description |
|---|---|
| `APPLE_CLIENT_ID` | Apple Services ID |
| `APPLE_TEAM_ID` | Apple Developer Team ID |
| `APPLE_KEY_ID` | Key ID for the signing key |
| `APPLE_PRIVATE_KEY` | PEM-encoded P8 private key content |

### Web (new)

| Variable | Description |
|---|---|
| `VITE_APPLE_CLIENT_ID` | Apple Services ID (controls button visibility) |

## Files Changed

| File | Change |
|---|---|
| `internal/auth/apple.go` | **New** — Apple JWT client secret generation |
| `internal/auth/oidc.go` | Add Apple-specific fields to `ProviderConfig` |
| `internal/relay/authflow.go` | **New** — Auth session store + `/api/auth/*` handlers |
| `internal/relay/server.go` | Register new auth routes |
| `cmd/relay/main.go` | Register Apple provider, wire auth flow handlers |
| `internal/cli/login.go` | Add relay-mediated browser flow, keep device code behind `--device-code` |
| `cmd/phosphor/main.go` | Add `--device-code` flag to login command |
| `web/src/auth/AuthProvider.tsx` | Add apple provider config, relay-mediated login for Apple |
| `web/src/auth/AuthCallback.tsx` | Add apple provider config |
| `web/src/components/Layout.tsx` | Add Apple sign-in button |
| `web/src/components/ProtectedRoute.tsx` | Add Apple sign-in button |
