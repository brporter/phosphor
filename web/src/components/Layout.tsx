import { Outlet, Link } from "react-router";
import { useAuth } from "../auth/useAuth";
import { ProviderButtons } from "./ProviderButtons";

export function Layout() {
  const { user, providers, login, logout } = useAuth();

  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        height: "100vh",
      }}
    >
      {/* Header */}
      <header
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          padding: "8px 16px",
          borderBottom: "1px solid var(--border)",
          background: "var(--bg-panel)",
          flexShrink: 0,
        }}
      >
        <Link
          to="/"
          style={{
            fontSize: 18,
            fontWeight: 700,
            color: "var(--green)",
            textDecoration: "none",
            textShadow: "0 0 10px var(--green-glow)",
          }}
        >
          {">"} phosphor
        </Link>

        <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
          <a
            href="https://github.com/brporter/phosphor/releases"
            target="_blank"
            rel="noopener noreferrer"
            style={{ color: "var(--text)", fontSize: 12, textDecoration: "none" }}
          >
            releases
          </a>
          {user ? (
            <>
              <span style={{ color: "var(--text)", fontSize: 12 }}>
                {user.profile?.email ?? user.profile?.sub ?? "signed in"}
              </span>
              <button onClick={() => void logout()}>logout</button>
            </>
          ) : (
            <ProviderButtons providers={providers} login={login} />
          )}
        </div>
      </header>

      {/* Main content */}
      <main style={{ flex: 1, overflow: "auto", padding: 16 }}>
        <Outlet />
      </main>

      {/* Footer */}
      <footer
        style={{
          padding: "4px 16px",
          borderTop: "1px solid var(--border)",
          fontSize: 11,
          color: "var(--border)",
          textAlign: "center",
          flexShrink: 0,
        }}
      >
        phosphor v0.1.0 | share your terminal
      </footer>
    </div>
  );
}
