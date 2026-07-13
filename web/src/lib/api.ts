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

export interface ApiKeyResponse {
  api_key: string;
  key_id: string;
}

export async function generateApiKey(token: string | null): Promise<ApiKeyResponse> {
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
  };
  if (token) {
    headers["Authorization"] = `Bearer ${token}`;
  }

  const res = await fetch(`${BASE}/api/auth/api-key`, {
    method: "POST",
    headers,
  });
  if (!res.ok) {
    throw new Error(`Failed to generate API key: ${res.status}`);
  }
  return res.json() as Promise<ApiKeyResponse>;
}
