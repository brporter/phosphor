```
 ██████╗ ██╗  ██╗ ██████╗ ███████╗██████╗ ██╗  ██╗ ██████╗ ██████╗
 ██╔══██╗██║  ██║██╔═══██╗██╔════╝██╔══██╗██║  ██║██╔═══██╗██╔══██╗
 ██████╔╝███████║██║   ██║███████╗██████╔╝███████║██║   ██║██████╔╝
 ██╔═══╝ ██╔══██║██║   ██║╚════██║██╔═══╝ ██╔══██║██║   ██║██╔══██╗
 ██║     ██║  ██║╚██████╔╝███████║██║     ██║  ██║╚██████╔╝██║  ██║
 ╚═╝     ╚═╝  ╚═╝ ╚═════╝ ╚══════╝╚═╝     ╚═╝  ╚═╝ ╚═════╝ ╚═╝  ╚═╝
```
What if all of your machines were accessible wherever you went, without exposing SSH ports to the internet? What if machines in your homelab, behind dynamic IP's, could be accessed from your phone, tablet, or laptop, wherever you were on earth? Securely? Without complicated VPN setups?

This is why I build Phosphor.

Phosphor gives you browser-based SSH access to your machines, wherever they are.

Phosphor inverts the traditional terminal access model. Instead of SSH'ing into a machine directly, your machines open a reverse SSH tunnel to a Phosphor hub. You sign in to the hub with federated credentials from Google, Microsoft, or Apple, see the machines associated with your account, and connect to one — an SSH client running in your browser (compiled to WebAssembly) connects **through** the hub to your machine's SSH daemon.

Perfect for developers, system administrators, and enthusiasts who need terminal access to multiple machines without exposing those machines to the internet. Access your homelab or cloud resources from anywhere, without configuring a VPN or opening SSH ports to the open web.

Phosphor is open source and completely self-hostable. Point the CLI at a relay with `--relay https://your-relay-server`; host your own hub or use a shared one.

# End-to-End Encryption

Phosphor's browser SSH client performs a real SSH handshake **end-to-end** with your machine's SSH daemon. The hub sits in the middle of the network path, but it only pipes the encrypted SSH stream between your browser and your machine — **it never sees your keystrokes, terminal output, or credentials in the clear.**

This is a significant change from earlier versions: there are no pre-shared secrets to manage, and a malicious or compromised relay cannot read your session contents. What a relay *can* do is attempt to open connections to an enrolled machine's SSH daemon (it can't authenticate without your key or password, but it is on the network path), so you should still run against a relay you trust or one you host yourself. Host key verification (trust-on-first-use, with fingerprint pinning in your browser) protects against the relay impersonating your machine.

The relay at phosphor.betaporter.dev is a shared hub I run personally. Whether you trust it is up to you — Phosphor is open source and runs on a cheap (or free-tier) VPS.

# Getting Started

Install Phosphor from the [releases page](https://github.com/brporter/phosphor/releases) for your operating system. Each machine you want to reach must run an SSH daemon (OpenSSH; on Windows, enable OpenSSH Server).

**1. Enroll the machine.** This authenticates you (a browser window opens for your identity provider), generates an SSH machine key, and registers the machine under your account:

```
phosphor enroll --relay https://phosphor.betaporter.dev
```

For headless machines, create an API key in the web app (upper-right → `settings`) and pass it: `phosphor enroll --relay ... --api-key phk:...`.

**2. Start the tunnel.** This maintains the reverse SSH tunnel and reconnects automatically:

```
phosphor tunnel
```

To keep it running, wrap `phosphor tunnel` in a service manager (systemd on Linux, launchd on macOS, a Windows service). For example, a minimal systemd unit:

```ini
[Unit]
Description=Phosphor tunnel
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=/usr/local/bin/phosphor tunnel
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

**3. Connect from the browser.** Open your hub (e.g. https://phosphor.betaporter.dev), sign in, pick the machine, and connect. On first connect you'll verify the host key and authenticate to your machine's SSH daemon with a password or an SSH key.

# Authenticating to your machine

Because the SSH handshake is end-to-end, you authenticate to your machine's SSH daemon exactly as you would with any SSH client — the hub is not involved. Supported methods: **public key**, **password**, and **keyboard-interactive**.

For key-based auth, the web app can generate an ed25519 keypair in your browser (the private key stays in your browser's storage and never leaves the device). Copy the generated `authorized_keys` line onto the target machine's `~/.ssh/authorized_keys`, and you'll connect passwordlessly.

# Security

- **Session contents** are protected by end-to-end SSH encryption between your browser and your machine; the hub only pipes ciphertext.
- **Machine → hub** authentication uses an SSH machine key created at enrollment, pinned to the hub's host key (fetched over TLS).
- **User → hub** authentication uses federated OIDC (Google/Microsoft/Apple). The hub stores no passwords. Headless enrollment uses API keys (revocable JWTs).
- **Host key verification** is trust-on-first-use with fingerprint pinning in the browser, so the hub cannot silently impersonate your machine.

Self-host your own hub or use a shared one — always specify it with `--relay`.

