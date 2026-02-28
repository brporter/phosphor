import type { SessionData } from "../components/SessionCard";

const BASE = "";

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
