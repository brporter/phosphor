import { spawn, type ChildProcess } from "child_process";
import type { Page } from "@playwright/test";

const RELAY_URL = "ws://localhost:8080";
const RELAY_HTTP = "http://localhost:8080";
const CLI_CMD = "go";
const CLI_ARGS_BASE = ["run", "../cmd/phosphor", "--relay", RELAY_URL];

/**
 * Perform the dev-mode login flow via the relay API and seed
 * the browser's localStorage so the SPA considers the user authenticated.
 */
export async function devLogin(page: Page): Promise<void> {
  // 1. Initiate dev login via API
  const loginResp = await fetch(`${RELAY_HTTP}/api/auth/login`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ provider: "dev", source: "web" }),
  });
  const { session_id } = (await loginResp.json()) as {
    auth_url: string;
    session_id: string;
  };

  // 2. Poll for the completed token
  const pollResp = await fetch(
    `${RELAY_HTTP}/api/auth/poll?session=${session_id}`
  );
  const { id_token } = (await pollResp.json()) as {
    status: string;
    id_token: string;
  };

  // 3. Parse the JWT payload to build the user profile
  const payloadB64 = id_token.split(".")[1]!;
  const payload = JSON.parse(Buffer.from(payloadB64, "base64url").toString());

  const user = {
    id_token,
    profile: { sub: payload.sub, iss: payload.iss, email: payload.email },
  };

  // 4. Seed localStorage before navigating — the AuthProvider will pick it up.
  await page.addInitScript((serializedUser: string) => {
    localStorage.setItem("phosphor_user", serializedUser);
  }, JSON.stringify(user));
}

export interface SessionHandle {
  sessionId: string;
  process: ChildProcess;
}

function waitForSessionId(proc: ChildProcess): Promise<string> {
  return new Promise((resolve, reject) => {
    const timeout = setTimeout(() => {
      reject(new Error("Timed out waiting for session ID"));
    }, 30_000);

    let output = "";
    proc.stderr?.on("data", (data: Buffer) => {
      output += data.toString();
      // Look for "Session live: http://.../<sessionId>"
      const match = output.match(/Session live: .+\/session\/(\S+)/);
      if (match) {
        clearTimeout(timeout);
        resolve(match[1]!);
      }
    });

    proc.on("error", (err) => {
      clearTimeout(timeout);
      reject(err);
    });

    proc.on("exit", (code) => {
      clearTimeout(timeout);
      reject(new Error(`CLI exited early with code ${code}. Output: ${output}`));
    });
  });
}

export async function startEncryptedSession(
  key: string
): Promise<SessionHandle> {
  const args = [...CLI_ARGS_BASE, "--key", key, "--", "bash"];
  const proc = spawn(CLI_CMD, args, {
    cwd: process.cwd(),
    stdio: ["pipe", "pipe", "pipe"],
  });
  const sessionId = await waitForSessionId(proc);
  return { sessionId, process: proc };
}

export async function startUnencryptedSession(): Promise<SessionHandle> {
  const args = [...CLI_ARGS_BASE, "--", "bash"];
  const proc = spawn(CLI_CMD, args, {
    cwd: process.cwd(),
    stdio: ["pipe", "pipe", "pipe"],
  });
  const sessionId = await waitForSessionId(proc);
  return { sessionId, process: proc };
}

export function cleanup(proc: ChildProcess): void {
  try {
    proc.kill("SIGTERM");
  } catch {
    // already dead
  }
}
