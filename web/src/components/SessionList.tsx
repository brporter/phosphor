import { useSessions } from "../hooks/useSessions";
import { SessionCard } from "./SessionCard";

export function SessionList() {
  const { sessions, isLoading, error } = useSessions();

  if (isLoading) {
    return (
      <div style={{ color: "var(--green)", padding: 16 }}>
        Loading sessions...
      </div>
    );
  }

  if (error) {
    return (
      <div style={{ color: "var(--red)", padding: 16 }}>Error: {error}</div>
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
        <pre style={{ color: "var(--green-dim)", fontSize: 13 }}>
          {`$ phosphor -- bash
Session live: http://localhost:8080/session/abc123`}
        </pre>
        <p style={{ fontSize: 13 }}>
          No active sessions. Start one from your terminal:
        </p>
        <code
          style={{
            background: "var(--bg-card)",
            border: "1px solid var(--border)",
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
      <h2
        style={{
          color: "var(--green)",
          fontSize: 14,
          marginBottom: 16,
          fontWeight: 500,
        }}
      >
        Active Sessions [{sessions.length}]
      </h2>
      <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
        {sessions.map((s) => (
          <SessionCard key={s.id} session={s} />
        ))}
      </div>
    </div>
  );
}
