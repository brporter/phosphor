import type { ReactNode } from "react";
import { useAuth } from "../auth/useAuth";

export function ProtectedRoute({ children }: { children: ReactNode }) {
  const { user, isLoading, login } = useAuth();

  if (isLoading) {
    return (
      <div
        style={{
          display: "flex",
          justifyContent: "center",
          alignItems: "center",
          height: "100%",
          color: "var(--green)",
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
            textShadow: "0 0 10px var(--green-glow)",
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
        <p style={{ color: "var(--text)" }}>
          Sign in to view your terminal sessions
        </p>
        <div style={{ display: "flex", gap: 12 }}>
          <button
            className="btn-primary"
            onClick={() => void login("microsoft")}
          >
            sign in with Microsoft
          </button>
          <button onClick={() => void login("google")}>
            sign in with Google
          </button>
        </div>
      </div>
    );
  }

  return <>{children}</>;
}
