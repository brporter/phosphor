import { useState } from "react";
import { Link } from "react-router";
import { destroySession } from "../lib/api";

export interface SessionData {
  id: string;
  mode: string;
  cols: number;
  rows: number;
  command: string;
  hostname: string;
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
      <div className="card-crt" style={{ display: "flex" }}>
        <Link
          to={`/session/${session.id}`}
          style={{ display: "block", flex: 1, textDecoration: "none" }}
        >
          <div
            style={{
              display: "flex",
              alignItems: "center",
              gap: 10,
              marginBottom: 6,
            }}
          >
            <span
              style={{
                color: "var(--green)",
                fontWeight: 600,
                fontSize: 13,
                textShadow: "0 0 6px rgba(0,255,65,0.3)",
              }}
            >
              {session.hostname
                ? `${session.hostname}: ${session.command || session.mode}`
                : session.command || session.mode}
            </span>
            <span className="badge badge-amber">[{session.mode}]</span>
            {session.lazy && !session.process_running && !session.process_exited && (
              <span className="badge badge-green">[ready]</span>
            )}
            {session.process_exited && (
              <span className="badge badge-red">[exited]</span>
            )}
          </div>
          <div
            style={{
              display: "flex",
              gap: 12,
              fontSize: 11,
              color: "var(--text-dim)",
            }}
          >
            <span>viewers: {session.viewers}</span>
            <span>id: {session.id}</span>
          </div>
        </Link>
        <button
          className="btn-danger"
          onClick={() => setShowDestroyModal(true)}
          style={{ alignSelf: "center" }}
        >
          [destroy]
        </button>
      </div>

      {showDestroyModal && (
        <div
          onClick={() => setShowDestroyModal(false)}
          style={{
            position: "fixed",
            inset: 0,
            background: "rgba(0, 0, 0, 0.85)",
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            zIndex: 1000,
          }}
        >
          <div
            onClick={(e) => e.stopPropagation()}
            style={{
              background: "var(--bg-card-crt)",
              border: "1px solid var(--border-crt)",
              boxShadow: "inset 0 0 20px rgba(0, 255, 65, 0.02)",
              padding: 24,
              maxWidth: 420,
              display: "flex",
              flexDirection: "column",
              gap: 16,
            }}
          >
            <div
              style={{
                color: "var(--red)",
                fontWeight: "bold",
                fontSize: 14,
                textShadow: "0 0 6px rgba(255,51,51,0.4)",
              }}
            >
              // DESTROY SESSION
            </div>
            <div style={{ color: "var(--text)", fontSize: 13, lineHeight: 1.5 }}>
              This will permanently terminate the session and kill the remote process.
              It cannot be resumed or restarted. You will need to re-run{" "}
              <code style={{ color: "var(--green)", textShadow: "0 0 4px rgba(0,255,65,0.3)" }}>
                phosphor
              </code>{" "}
              on the remote machine to start a new session.
            </div>
            <div style={{ display: "flex", gap: 12, justifyContent: "flex-end" }}>
              <button className="btn-action" onClick={() => setShowDestroyModal(false)}>
                [cancel]
              </button>
              <button className="btn-danger" onClick={handleDestroy}>
                [destroy session]
              </button>
            </div>
          </div>
        </div>
      )}
    </>
  );
}
