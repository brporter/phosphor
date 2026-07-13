# VM deployment (shell.betaporter.dev)

The relay runs as a Docker Compose stack on a single Linux VM:

- **caddy** — terminates TLS for `phosphor.betaporter.dev` (automatic Let's Encrypt) and reverse-proxies HTTP + the `/ws/*` WebSocket routes to the relay. Caddy fronts HTTP/WS **only**; the raw SSH gateway port is exposed directly (see below).
- **relay** — the phosphor relay image, pulled from GHCR (`ghcr.io/brporter/phosphor-relay:latest`). The image is **public** (built entirely from this public repo; secrets are injected at runtime), so no registry credentials are needed. CI pushes a new `:latest` (plus a `:<sha>` tag for rollbacks) on every push to `main`. Listens on `:8080` (HTTP, behind Caddy) and `:2222` (SSH gateway, exposed directly). Persists its SSH host key on the `phosphor-etc` volume so the fingerprint is stable across redeploys.
- **postgres** — durable state (tenants, users, machines, API keys) on the `pg-data` volume.
- **watchtower** — polls GHCR every 5 minutes and restarts the relay when `:latest` changes. Scoped by label so it only touches the relay. Note: in-flight browser SSH sessions drop when the relay restarts; CLI tunnels auto-reconnect.

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
   and populate `POSTGRES_PASSWORD`, the OIDC credentials, and `API_KEY_SECRET`.
   `chmod 600 .env`.

5. **Open ports** in the VM's Azure NSG (and any host firewall):
   - **80, 443** — Caddy (HTTP/HTTPS + WebSocket).
   - **2222** — the SSH gateway that `phosphor tunnel` clients dial.

6. **Start the stack**:

   ```sh
   cd /opt/phosphor
   sudo docker compose up -d
   ```

7. **Point DNS at the VM**: in the `betaporter.dev` zone, set the `phosphor`
   CNAME record to `shell.betaporter.dev`. Caddy obtains the TLS certificate
   automatically once DNS resolves here. Confirm `SSH_PUBLIC_ADDR` in
   `docker-compose.yml` matches the hostname CLIs should dial (default
   `phosphor.betaporter.dev:2222`).

## Enrolling a machine

On any machine you want to reach:

```sh
phosphor enroll --relay https://phosphor.betaporter.dev
phosphor tunnel      # run under systemd/launchd to keep it alive
```

The machine must run an SSH daemon (OpenSSH; on Windows, enable OpenSSH Server).

## Operations

| Task | Command (from `/opt/phosphor`) |
|---|---|
| Status | `sudo docker compose ps` |
| Relay logs | `sudo docker compose logs -f relay` |
| Deploy log | `sudo docker compose logs -f watchtower` |
| Force an update now | `sudo docker compose pull relay` then `sudo docker compose up -d relay` |
| Roll back | edit `docker-compose.yml` to pin `phosphor-relay:<sha>`, then `sudo docker compose up -d relay` (re-pin `:latest` afterwards) |
| Back up the database | `sudo docker compose exec postgres pg_dump -U phosphor phosphor > backup.sql` |
| Restart everything | `sudo docker compose restart` |

Health check: `curl https://phosphor.betaporter.dev/health`
