import type { ReactNode } from "react";
import { useAuth } from "../auth/useAuth";
import { ProviderButtons } from "./ProviderButtons";

export function ProtectedRoute({ children }: { children: ReactNode }) {
  const { user, isLoading, providers, login } = useAuth();

  if (isLoading) {
    return (
      <div
        style={{
          display: "flex",
          justifyContent: "center",
          alignItems: "center",
          height: "100%",
          color: "var(--green)",
          textShadow: "0 0 6px rgba(0, 255, 65, 0.4)",
        }}
      >
        <span className="blink">Initializing...</span>
      </div>
    );
  }

  if (!user) {
    return (
      <div
        style={{
          display: "flex",
          flexDirection: "column",
          justifyContent: "center",
          alignItems: "center",
          height: "100%",
          gap: 24,
        }}
      >
        <pre
          style={{
            color: "var(--green)",
            textShadow: "0 0 12px rgba(0, 255, 65, 0.5)",
            fontSize: 16,
            textAlign: "center",
            lineHeight: 1.6,
          }}
        >
          {`
 ██████╗ ██╗  ██╗ ██████╗ ███████╗██████╗ ██╗  ██╗ ██████╗ ██████╗
 ██╔══██╗██║  ██║██╔═══██╗██╔════╝██╔══██╗██║  ██║██╔═══██╗██╔══██╗
 ██████╔╝███████║██║   ██║███████╗██████╔╝███████║██║   ██║██████╔╝
 ██╔═══╝ ██╔══██║██║   ██║╚════██║██╔═══╝ ██╔══██║██║   ██║██╔══██╗
 ██║     ██║  ██║╚██████╔╝███████║██║     ██║  ██║╚██████╔╝██║  ██║
 ╚═╝     ╚═╝  ╚═╝ ╚═════╝ ╚══════╝╚═╝     ╚═╝  ╚═╝ ╚═════╝ ╚═╝  ╚═╝`}
        </pre>
        <div className="section-heading">// AUTHENTICATION REQUIRED</div>
        <ProviderButtons providers={providers} login={login} />
      </div>
    );
  }

  return <>{children}</>;
}
