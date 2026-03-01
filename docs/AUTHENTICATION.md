# Authentication Configuration

Phosphor supports three OIDC identity providers: Microsoft (Entra ID), Google, and Apple. All authentication flows (CLI and web) go through the relay server, which handles OIDC token exchange server-side.

## Environment Variables Overview

### Relay Server

| Variable | Provider | Required | Description |
|---|---|---|---|
| `MICROSOFT_CLIENT_ID` | Microsoft | Yes | Application (client) ID from Entra ID |
| `MICROSOFT_CLIENT_SECRET` | Microsoft | No | Client secret (for confidential clients) |
| `GOOGLE_CLIENT_ID` | Google | Yes | OAuth 2.0 client ID |
| `GOOGLE_CLIENT_SECRET` | Google | No | OAuth 2.0 client secret |
| `APPLE_CLIENT_ID` | Apple | Yes | Services ID (not the App ID) |
| `APPLE_TEAM_ID` | Apple | Yes | 10-character Apple Developer Team ID |
| `APPLE_KEY_ID` | Apple | Yes | Key ID for the Sign in with Apple private key |
| `APPLE_PRIVATE_KEY` | Apple | Yes | PEM-encoded contents of the `.p8` private key file |
| `BASE_URL` | All | Yes | Public URL of the relay (e.g. `https://phosphor.example.com`) |
| `DEV_MODE` | All | No | Set to any value to bypass authentication entirely |

The web frontend does not require any provider-specific environment variables. All OIDC configuration lives on the relay server.

### CLI

| Variable | Provider | Description |
|---|---|---|
| `PHOSPHOR_MICROSOFT_CLIENT_ID` | Microsoft | Client ID for device code flow |
| `PHOSPHOR_GOOGLE_CLIENT_ID` | Google | Client ID for device code flow |

The CLI does not need Apple-specific env vars. It authenticates through the relay's browser-based flow.

---

## Microsoft (Entra ID)

### 1. Register an application in Azure

1. Go to [Azure Portal](https://portal.azure.com) > **Microsoft Entra ID** > **App registrations** > **New registration**.
2. Set **Name** to `Phosphor`.
3. Under **Supported account types**, select **Accounts in any organizational directory and personal Microsoft accounts**.
4. Under **Redirect URIs**, add a **Web** platform redirect:
   ```
   https://your-relay-domain.com/api/auth/callback
   ```
   For local development, also add:
   ```
   http://localhost:8080/api/auth/callback
   http://localhost:3000/auth/callback
   ```
5. Click **Register**.

### 2. Configure the application

1. On the app's **Overview** page, copy the **Application (client) ID**.
2. Go to **Certificates & secrets** > **New client secret**. Copy the secret value.
3. Go to **Authentication**:
   - Enable **Allow public client flows** (required for device code flow on the CLI).
   - Under **Implicit grant and hybrid flows**, ensure **ID tokens** is checked.
4. Go to **API permissions**. Ensure these delegated permissions are present (they should be by default):
   - `openid`
   - `profile`
   - `email`

### 3. Set environment variables

Relay server:
```bash
export MICROSOFT_CLIENT_ID="your-application-client-id"
export MICROSOFT_CLIENT_SECRET="your-client-secret"
```

CLI (for device code flow):
```bash
export PHOSPHOR_MICROSOFT_CLIENT_ID="your-application-client-id"
```

---

## Google

### 1. Create OAuth credentials

1. Go to [Google Cloud Console](https://console.cloud.google.com) > **APIs & Services** > **Credentials** > **Create Credentials** > **OAuth client ID**.
2. If prompted, configure the **OAuth consent screen** first:
   - Choose **External** user type.
   - Add the scopes: `openid`, `email`, `profile`.
   - Add your domain to **Authorized domains**.
3. Back in **Create OAuth client ID**:
   - Application type: **Web application**.
   - Name: `Phosphor`.
   - **Authorized redirect URIs**: add:
     ```
     https://your-relay-domain.com/api/auth/callback
     ```
     For local development, also add:
     ```
     http://localhost:8080/api/auth/callback
     http://localhost:3000/auth/callback
     ```
4. Copy the **Client ID** and **Client secret**.

### 2. Enable device code flow (optional, for CLI)

Device code flow requires a separate OAuth client:

1. Create another OAuth client ID with application type **TVs and Limited Input devices**.
2. Copy this client ID for use with the CLI.

Alternatively, CLI users can use the browser-based login (default) which uses the same web client ID.

### 3. Set environment variables

Relay server:
```bash
export GOOGLE_CLIENT_ID="your-client-id.apps.googleusercontent.com"
export GOOGLE_CLIENT_SECRET="your-client-secret"
```

CLI (only needed for `--device-code` flow):
```bash
export PHOSPHOR_GOOGLE_CLIENT_ID="your-tv-client-id.apps.googleusercontent.com"
```

---

## Apple

Apple's Sign in with Apple requires an Apple Developer Program membership ($99/year).

### 1. Register an App ID

1. Go to [Apple Developer](https://developer.apple.com/account) > **Certificates, Identifiers & Profiles** > **Identifiers**.
2. Click **+** to register a new identifier. Choose **App IDs**, then **App**.
3. Enter a description (e.g. `Phosphor`) and a Bundle ID (e.g. `com.yourorg.phosphor`).
4. Under **Capabilities**, enable **Sign in with Apple**.
5. Click **Continue** and **Register**.

### 2. Create a Services ID

The Services ID is used as the OAuth `client_id` for web-based Sign in with Apple.

1. Under **Identifiers**, click **+** again. Choose **Services IDs**.
2. Enter a description (e.g. `Phosphor Web`) and an identifier (e.g. `com.yourorg.phosphor.web`).
3. Click **Continue** and **Register**.
4. Click into the newly created Services ID and enable **Sign in with Apple**.
5. Click **Configure** next to Sign in with Apple:
   - **Primary App ID**: select the App ID created in step 1.
   - **Domains and Subdomains**: add your relay's domain (e.g. `phosphor.example.com`). For local development, `localhost` will not work — you must use a tunnel or real domain.
   - **Return URLs**: add:
     ```
     https://your-relay-domain.com/api/auth/callback
     ```
6. Click **Save** and then **Continue** and **Register**.
7. Copy the **Identifier** (e.g. `com.yourorg.phosphor.web`) — this is your `APPLE_CLIENT_ID`.

### 3. Create a Sign in with Apple key

1. Under **Keys**, click **+** to register a new key.
2. Enter a name (e.g. `Phosphor Sign In`).
3. Enable **Sign in with Apple** and click **Configure**.
4. Select the Primary App ID from step 1.
5. Click **Save**, then **Continue**, then **Register**.
6. **Download the `.p8` key file** — you can only download it once.
7. Note the **Key ID** displayed on the confirmation page.

### 4. Find your Team ID

Your **Team ID** is the 10-character identifier shown in the top-right of the Apple Developer portal, or under **Membership Details**.

### 5. Set environment variables

The `APPLE_PRIVATE_KEY` variable must contain the full PEM contents of the `.p8` file, including the `-----BEGIN PRIVATE KEY-----` and `-----END PRIVATE KEY-----` lines.

Relay server:
```bash
export APPLE_CLIENT_ID="com.yourorg.phosphor.web"
export APPLE_TEAM_ID="ABCDE12345"
export APPLE_KEY_ID="KEYID12345"
export APPLE_PRIVATE_KEY="$(cat path/to/AuthKey_KEYID12345.p8)"
```

The CLI does not need Apple-specific variables — it authenticates via the relay's browser flow.

---

## CLI Authentication

The CLI supports two login methods:

### Browser-based login (default, all providers)

Opens your browser, authenticates via the relay server, and caches the token locally.

```bash
# Login with Microsoft (default)
phosphor login

# Login with a specific provider
phosphor login --provider apple
phosphor login --provider google
phosphor login --provider microsoft
```

The CLI must be able to reach the relay server. Use `--relay` to specify a non-default relay URL:

```bash
phosphor login --provider apple --relay wss://phosphor.example.com
```

### Device code flow (Microsoft and Google only)

For environments without a browser (SSH sessions, headless machines). Not available for Apple.

```bash
phosphor login --provider microsoft --device-code
phosphor login --provider google --device-code
```

This displays a URL and code to enter on any device with a browser.

### Token cache

Tokens are cached at `~/.config/phosphor/tokens.json`. To clear:

```bash
phosphor logout
```

---

## Local Development

For local development without configuring real OIDC providers, set `DEV_MODE`:

```bash
DEV_MODE=1 make dev-relay
```

This bypasses all authentication. The web frontend will use a dev mode fallback when provider client IDs are not set.

To test with real providers locally, set the environment variables and ensure redirect URIs include `http://localhost:8080/api/auth/callback` in your provider's app registration.

Note: Apple does not allow `localhost` as a redirect domain. To test Apple Sign in locally, use a tunneling service (e.g. ngrok) to expose your local relay on a real domain, and register that domain in your Apple Services ID configuration.

---

## Production Deployment

When deploying with Docker, pass environment variables to the container:

```bash
docker run -p 8080:8080 \
  -e BASE_URL=https://phosphor.example.com \
  -e MICROSOFT_CLIENT_ID=... \
  -e MICROSOFT_CLIENT_SECRET=... \
  -e GOOGLE_CLIENT_ID=... \
  -e GOOGLE_CLIENT_SECRET=... \
  -e APPLE_CLIENT_ID=... \
  -e APPLE_TEAM_ID=... \
  -e APPLE_KEY_ID=... \
  -e APPLE_PRIVATE_KEY="$(cat AuthKey.p8)" \
  phosphor-relay
```

For Azure Container Apps, the `deploy/azure/main.bicep` template accepts `microsoftClientId`, `microsoftClientSecret`, `googleClientId`, and `googleClientSecret` as parameters. Apple parameters need to be added to the Bicep template if deploying Apple auth via Azure.
