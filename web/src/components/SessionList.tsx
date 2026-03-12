import { useAuth } from "../auth/useAuth";
import { useSessions } from "../hooks/useSessions";
import { SessionCard } from "./SessionCard";

export function SessionList() {
  const { getToken } = useAuth();
  const { sessions, isLoading, error, refresh } = useSessions();

  if (isLoading) {
    return (
      <div
        style={{
          color: "var(--green)",
          textShadow: "0 0 6px rgba(0,255,65,0.4)",
          padding: 16,
        }}
      >
        Loading sessions...
      </div>
    );
  }

  if (error) {
    return (
      <div
        style={{
          color: "var(--red)",
          textShadow: "0 0 6px rgba(255,51,51,0.4)",
          padding: 16,
        }}
      >
        Error: {error}
      </div>
    );
  }

  if (sessions.length === 0) {
    return (
      <div
        style={{
          display: "flex",
          flexDirection: "column",
          alignItems: "center",
          justifyContent: "center",
          height: "100%",
          gap: 16,
          color: "var(--text)",
        }}
      >
        <pre
          style={{
            color: "var(--green-dim)",
            fontSize: 13,
            textShadow: "0 0 6px rgba(0,255,65,0.4)",
          }}
        >
          {`$ phosphor -- bash
Session live: http://localhost:8080/session/abc123`}
        </pre>
        <p style={{ fontSize: 13 }}>
          No active sessions. Start one from your terminal:
        </p>
        <code
          style={{
            background: "var(--bg-card-crt)",
            border: "1px solid var(--border-crt)",
            padding: "8px 16px",
            color: "var(--amber)",
          }}
        >
          phosphor -- bash
        </code>
      </div>
    );
  }

  return (
    <div>
      <div
        className="section-heading"
        style={{ marginBottom: 16 }}
      >
        // ACTIVE SESSIONS =============== [{sessions.length}]
      </div>
      <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
        {sessions.map((s) => (
          <SessionCard key={s.id} session={s} token={getToken()} onDestroyed={refresh} />
        ))}
      </div>
    </div>
  );
}
