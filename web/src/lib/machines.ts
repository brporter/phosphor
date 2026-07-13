// REST client for the machines API.

export interface Machine {
  id: string;
  name: string;
  hostname: string;
  fingerprint: string;
  online: boolean;
  created_at: string;
  last_seen_at: string | null;
}

export interface SSHInfo {
  addr: string;
  host_key: string;
  fingerprint: string;
}

function authHeaders(token: string | null): Record<string, string> {
  const headers: Record<string, string> = { "Content-Type": "application/json" };
  if (token) headers["Authorization"] = `Bearer ${token}`;
  return headers;
}

export async function fetchMachines(token: string | null): Promise<Machine[]> {
  const res = await fetch("/api/machines", { headers: authHeaders(token) });
  if (!res.ok) throw new Error(`Failed to fetch machines: ${res.status}`);
  return res.json() as Promise<Machine[]>;
}

export async function renameMachine(
  id: string,
  name: string,
  token: string | null,
): Promise<Machine> {
  const res = await fetch(`/api/machines/${id}`, {
    method: "PATCH",
    headers: authHeaders(token),
    body: JSON.stringify({ name }),
  });
  if (!res.ok) throw new Error(`Failed to rename machine: ${res.status}`);
  return res.json() as Promise<Machine>;
}

export async function deleteMachine(id: string, token: string | null): Promise<void> {
  const res = await fetch(`/api/machines/${id}`, {
    method: "DELETE",
    headers: authHeaders(token),
  });
  if (!res.ok && res.status !== 204) {
    throw new Error(`Failed to delete machine: ${res.status}`);
  }
}

export async function fetchSSHInfo(token: string | null): Promise<SSHInfo> {
  const res = await fetch("/api/ssh-info", { headers: authHeaders(token) });
  if (!res.ok) throw new Error(`Failed to fetch ssh info: ${res.status}`);
  return res.json() as Promise<SSHInfo>;
}
