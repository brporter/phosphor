import {
  createContext,
  useCallback,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { UserManager, type User } from "oidc-client-ts";

interface AuthContextValue {
  user: User | null;
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

// OIDC provider configurations
const providerConfigs: Record<
  string,
  { authority: string; client_id_env: string; scope: string }
> = {
  microsoft: {
    authority: "https://login.microsoftonline.com/common/v2.0",
    client_id_env: "VITE_MICROSOFT_CLIENT_ID",
    scope: "openid profile email",
  },
  google: {
    authority: "https://accounts.google.com",
    client_id_env: "VITE_GOOGLE_CLIENT_ID",
    scope: "openid profile email",
  },
};

function createUserManager(provider: string): UserManager | null {
  const config = providerConfigs[provider];
  if (!config) return null;

  const clientId = import.meta.env[config.client_id_env] as string | undefined;
  if (!clientId) return null;

  return new UserManager({
    authority: config.authority,
    client_id: clientId,
    redirect_uri: `${window.location.origin}/auth/callback`,
    post_logout_redirect_uri: window.location.origin,
    scope: config.scope,
    response_type: "code",
  });
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [isLoading, setIsLoading] = useState(true);

  // Try to restore session on mount
  useEffect(() => {
    const provider = localStorage.getItem("phosphor_provider") ?? "microsoft";
    const mgr = createUserManager(provider);
    if (mgr) {
      mgr
        .getUser()
        .then((u) => setUser(u))
        .finally(() => setIsLoading(false));
    } else {
      // Dev mode: no OIDC configured
      setIsLoading(false);
    }
  }, []);

  const login = useCallback(async (provider: string) => {
    localStorage.setItem("phosphor_provider", provider);
    const mgr = createUserManager(provider);
    if (!mgr) {
      // Dev mode fallback
      setUser({
        id_token: "dev:anonymous",
        access_token: "dev:anonymous",
        token_type: "Bearer",
        profile: { sub: "anonymous", iss: "dev" },
        expired: false,
      } as unknown as User);
      return;
    }
    await mgr.signinRedirect();
  }, []);

  const logout = useCallback(async () => {
    const provider = localStorage.getItem("phosphor_provider") ?? "microsoft";
    const mgr = createUserManager(provider);
    setUser(null);
    if (mgr) {
      await mgr.signoutRedirect();
    }
  }, []);

  const getToken = useCallback(() => {
    return user?.id_token ?? user?.access_token ?? null;
  }, [user]);

  const value = useMemo(
    () => ({ user, isLoading, login, logout, getToken }),
    [user, isLoading, login, logout, getToken],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}
