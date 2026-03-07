import type { SessionData } from "../components/SessionCard";

const BASE = "";

export interface AuthConfig {
  providers: string[];
}

export async function fetchAuthConfig(): Promise<AuthConfig> {
  const res = await fetch(`${BASE}/api/auth/config`);
  if (!res.ok) {
    return { providers: [] };
  }
  return res.json() as Promise<AuthConfig>;
}

export async function destroySession(id: string, token: string | null): Promise<void> {
  const headers: Record<string, string> = {};
  if (token) {
    headers["Authorization"] = `Bearer ${token}`;
  }

  const res = await fetch(`${BASE}/api/sessions/${id}`, {
    method: "DELETE",
    headers,
  });
  if (!res.ok) {
    throw new Error(`Failed to destroy session: ${res.status}`);
  }
}

export async function fetchSessions(token: string | null): Promise<SessionData[]> {
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
  };
  if (token) {
    headers["Authorization"] = `Bearer ${token}`;
  }

  const res = await fetch(`${BASE}/api/sessions`, { headers });
  if (!res.ok) {
    throw new Error(`Failed to fetch sessions: ${res.status}`);
  }
  return res.json() as Promise<SessionData[]>;
}
