import {
  createContext,
  useCallback,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";

interface UserProfile {
  sub: string;
  iss: string;
  email?: string;
}

interface AuthUser {
  id_token: string;
  profile: UserProfile;
}

interface AuthContextValue {
  user: AuthUser | null;
  isLoading: boolean;
  login: (provider: string) => Promise<void>;
  logout: () => Promise<void>;
  getToken: () => string | null;
}

export const AuthContext = createContext<AuthContextValue>({
  user: null,
  isLoading: true,
  login: async () => {},
  logout: async () => {},
  getToken: () => null,
});

const STORAGE_KEY = "phosphor_user";
const SESSION_KEY = "phosphor_auth_session";

function parseJwtPayload(token: string): UserProfile {
  const parts = token.split(".");
  const payload = JSON.parse(atob(parts[1] ?? ""));
  return { sub: payload.sub, iss: payload.iss, email: payload.email };
}

function saveUser(user: AuthUser) {
  localStorage.setItem(STORAGE_KEY, JSON.stringify(user));
}

function loadUser(): AuthUser | null {
  const raw = localStorage.getItem(STORAGE_KEY);
  if (!raw) return null;
  try {
    const user = JSON.parse(raw) as AuthUser;
    // Check token expiry
    const parts = user.id_token.split(".");
    const payload = JSON.parse(atob(parts[1] ?? ""));
    if (payload.exp && payload.exp * 1000 < Date.now()) {
      localStorage.removeItem(STORAGE_KEY);
      return null;
    }
    return user;
  } catch {
    localStorage.removeItem(STORAGE_KEY);
    return null;
  }
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<AuthUser | null>(null);
  const [isLoading, setIsLoading] = useState(true);

  useEffect(() => {
    // Check for pending auth session (returning from provider redirect)
    const sessionId = localStorage.getItem(SESSION_KEY);
    if (sessionId) {
      localStorage.removeItem(SESSION_KEY);
      const poll = async () => {
        const resp = await fetch(`/api/auth/poll?session=${sessionId}`);
        const data = await resp.json();
        if (data.status === "complete" && data.id_token) {
          const profile = parseJwtPayload(data.id_token);
          const u: AuthUser = { id_token: data.id_token, profile };
          saveUser(u);
          setUser(u);
        }
        setIsLoading(false);
      };
      poll();
      return;
    }

    // Restore cached session
    const cached = loadUser();
    setUser(cached);
    setIsLoading(false);
  }, []);

  const login = useCallback(async (provider: string) => {
    const resp = await fetch("/api/auth/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ provider }),
    });

    if (!resp.ok) {
      throw new Error(`Login failed: ${resp.status}`);
    }

    const { auth_url, session_id } = await resp.json();
    localStorage.setItem(SESSION_KEY, session_id);
    window.location.href = auth_url;
  }, []);

  const logout = useCallback(async () => {
    localStorage.removeItem(STORAGE_KEY);
    setUser(null);
  }, []);

  const getToken = useCallback(() => {
    return user?.id_token ?? null;
  }, [user]);

  const value = useMemo(
    () => ({ user, isLoading, login, logout, getToken }),
    [user, isLoading, login, logout, getToken],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}
