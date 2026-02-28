import { useEffect, useState } from "react";
import { useNavigate } from "react-router";
import { UserManager } from "oidc-client-ts";

export function AuthCallback() {
  const navigate = useNavigate();
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const provider = localStorage.getItem("phosphor_provider") ?? "microsoft";
    const configs: Record<string, { authority: string; client_id_env: string }> =
      {
        microsoft: {
          authority: "https://login.microsoftonline.com/common/v2.0",
          client_id_env: "VITE_MICROSOFT_CLIENT_ID",
        },
        google: {
          authority: "https://accounts.google.com",
          client_id_env: "VITE_GOOGLE_CLIENT_ID",
        },
      };

    const config = configs[provider];
    if (!config) {
      setError("Unknown provider");
      return;
    }

    const clientId = import.meta.env[config.client_id_env] as
      | string
      | undefined;
    if (!clientId) {
      setError("Client ID not configured");
      return;
    }

    const mgr = new UserManager({
      authority: config.authority,
      client_id: clientId,
      redirect_uri: `${window.location.origin}/auth/callback`,
      scope: "openid profile email",
      response_type: "code",
    });

    mgr
      .signinRedirectCallback()
      .then(() => navigate("/", { replace: true }))
      .catch((err) => setError(String(err)));
  }, [navigate]);

  if (error) {
    return (
      <div style={{ padding: "2em", color: "var(--red)" }}>
        <h2>Authentication Error</h2>
        <p>{error}</p>
        <a href="/">Return home</a>
      </div>
    );
  }

  return (
    <div style={{ padding: "2em", color: "var(--green)" }}>
      Completing sign-in...
    </div>
  );
}
