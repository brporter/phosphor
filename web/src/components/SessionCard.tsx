import { Link } from "react-router";

export interface SessionData {
  id: string;
  mode: string;
  cols: number;
  rows: number;
  command: string;
  viewers: number;
}

export function SessionCard({ session }: { session: SessionData }) {
  return (
    <Link
      to={`/session/${session.id}`}
      style={{
        display: "block",
        border: "1px solid var(--border)",
        background: "var(--bg-card)",
        padding: 16,
        textDecoration: "none",
        transition: "all 0.15s",
      }}
      onMouseEnter={(e) => {
        e.currentTarget.style.borderColor = "var(--green-dim)";
        e.currentTarget.style.boxShadow = "0 0 12px var(--green-glow)";
      }}
      onMouseLeave={(e) => {
        e.currentTarget.style.borderColor = "var(--border)";
        e.currentTarget.style.boxShadow = "none";
      }}
    >
      <div
        style={{
          display: "flex",
          justifyContent: "space-between",
          alignItems: "baseline",
          marginBottom: 8,
        }}
      >
        <span style={{ color: "var(--green)", fontWeight: 700 }}>
          {session.command || session.mode}
        </span>
        <span
          style={{
            fontSize: 11,
            color: "var(--amber)",
            border: "1px solid var(--amber-dim)",
            padding: "2px 6px",
          }}
        >
          {session.mode}
        </span>
      </div>
      <div
        style={{
          display: "flex",
          gap: 16,
          fontSize: 12,
          color: "var(--text)",
        }}
      >
        <span>
          {session.cols}x{session.rows}
        </span>
        <span>
          {session.viewers} viewer{session.viewers !== 1 ? "s" : ""}
        </span>
        <span style={{ color: "var(--border)" }}>{session.id}</span>
      </div>
    </Link>
  );
}
