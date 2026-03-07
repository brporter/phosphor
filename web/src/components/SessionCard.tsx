import { useState } from "react";
import { Link } from "react-router";
import { destroySession } from "../lib/api";

export interface SessionData {
  id: string;
  mode: string;
  cols: number;
  rows: number;
  command: string;
  viewers: number;
  process_exited: boolean;
  lazy: boolean;
  process_running: boolean;
}

interface SessionCardProps {
  session: SessionData;
  token: string | null;
  onDestroyed: () => void;
}

export function SessionCard({ session, token, onDestroyed }: SessionCardProps) {
  const [showDestroyModal, setShowDestroyModal] = useState(false);

  const handleDestroy = async () => {
    try {
      await destroySession(session.id, token);
    } catch {
      // Session may already be gone
    }
    onDestroyed();
  };

  return (
    <>
      <div
        style={{
          display: "flex",
          border: "1px solid var(--border)",
          background: "var(--bg-card)",
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
        <Link
          to={`/session/${session.id}`}
          style={{
            display: "block",
            flex: 1,
            padding: 16,
            textDecoration: "none",
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
            {session.lazy && !session.process_running && !session.process_exited && (
              <span
                style={{
                  fontSize: 11,
                  color: "var(--green)",
                  border: "1px solid var(--green-dim)",
                  padding: "2px 6px",
                  marginLeft: 4,
                }}
              >
                ready
              </span>
            )}
            {session.process_exited && (
              <span
                style={{
                  fontSize: 11,
                  color: "var(--red)",
                  border: "1px solid var(--red)",
                  padding: "2px 6px",
                  marginLeft: 4,
                }}
              >
                exited
              </span>
            )}
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
              {session.viewers} viewer{session.viewers !== 1 ? "s" : ""}
            </span>
            <span style={{ color: "var(--border)" }}>{session.id}</span>
          </div>
        </Link>
        <button
          onClick={() => setShowDestroyModal(true)}
          style={{
            color: "var(--red)",
            cursor: "pointer",
            padding: "0 16px",
            fontSize: 12,
            alignSelf: "center",
          }}
        >
          destroy
        </button>
      </div>

      {showDestroyModal && (
        <div
          onClick={() => setShowDestroyModal(false)}
          style={{
            position: "fixed",
            inset: 0,
            background: "rgba(0, 0, 0, 0.8)",
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            zIndex: 1000,
          }}
        >
          <div
            onClick={(e) => e.stopPropagation()}
            style={{
              background: "var(--bg-card)",
              border: "1px solid var(--border)",
              padding: 24,
              maxWidth: 420,
              display: "flex",
              flexDirection: "column",
              gap: 16,
            }}
          >
            <div style={{ color: "var(--red)", fontWeight: "bold", fontSize: 14 }}>
              Destroy Session
            </div>
            <div style={{ color: "var(--text)", fontSize: 13, lineHeight: 1.5 }}>
              This will permanently terminate the session and kill the remote process.
              It cannot be resumed or restarted. You will need to re-run{" "}
              <code style={{ color: "var(--green)" }}>phosphor</code> on the remote
              machine to start a new session.
            </div>
            <div style={{ display: "flex", gap: 12, justifyContent: "flex-end" }}>
              <button onClick={() => setShowDestroyModal(false)}>cancel</button>
              <button
                onClick={handleDestroy}
                style={{
                  color: "var(--red)",
                  borderColor: "var(--red)",
                }}
              >
                destroy session
              </button>
            </div>
          </div>
        </div>
      )}
    </>
  );
}
