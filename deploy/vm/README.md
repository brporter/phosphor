# VM deployment (shell.betaporter.dev)

The relay runs as a Docker Compose stack on a single Linux VM:

- **caddy** — terminates TLS for `phosphor.betaporter.dev` (automatic Let's Encrypt) and reverse-proxies to the relay, including the `/ws/*` WebSocket routes.
- **relay** — the phosphor relay image, pulled from GHCR (`ghcr.io/brporter/phosphor-relay:latest`). The image is **public** (built entirely from this public repo; secrets are injected at runtime), so no registry credentials are needed. CI pushes a new `:latest` (plus a `:<sha>` tag for rollbacks) on every push to `main`.
- **redis** — local session/auth store (replaces Azure Cache for Redis).
- **watchtower** — polls GHCR every 5 minutes and restarts the relay when `:latest` changes. Scoped by label so it only touches the relay. Note: active terminal sessions drop when the relay restarts.

## One-time setup

1. **Make the GHCR package public** (once, after CI's first push creates it):
   GitHub → your profile → Packages → `phosphor-relay` → Package settings →
   Danger Zone → Change visibility → Public.

2. **Install Docker Engine + the compose plugin** — follow
   <https://docs.docker.com/engine/install/> for your distro (use the official
   Docker apt repo, not the distro package).

3. **Create the deploy directory** and copy this directory's files there:

   ```sh
   sudo mkdir -p /opt/phosphor
   sudo cp docker-compose.yml Caddyfile .env-template /opt/phosphor/
   ```

4. **Fill in secrets**: `cd /opt/phosphor`, copy `.env-template` to `.env`,
   and populate the OIDC credentials and `API_KEY_SECRET`. `chmod 600 .env`.

5. **Open ports 80 and 443** in the VM's Azure NSG (and any host firewall).

6. **Start the stack**:

   ```sh
   cd /opt/phosphor
   sudo docker compose up -d
   ```

7. **Point DNS at the VM**: in the `betaporter.dev` zone, set the `phosphor`
   CNAME record to `shell.betaporter.dev`. Caddy obtains the TLS certificate
   automatically once DNS resolves here.

## Operations

| Task | Command (from `/opt/phosphor`) |
|---|---|
| Status | `sudo docker compose ps` |
| Relay logs | `sudo docker compose logs -f relay` |
| Deploy log | `sudo docker compose logs -f watchtower` |
| Force an update now | `sudo docker compose pull relay` then `sudo docker compose up -d relay` |
| Roll back | edit `docker-compose.yml` to pin `phosphor-relay:<sha>`, then `sudo docker compose up -d relay` (re-pin `:latest` afterwards) |
| Restart everything | `sudo docker compose restart` |

Health check: `curl https://phosphor.betaporter.dev/health`
