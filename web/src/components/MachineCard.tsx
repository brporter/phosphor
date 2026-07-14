import { useState } from "react";
import { useNavigate } from "react-router";
import { deleteMachine, type Machine } from "../lib/machines";

interface MachineCardProps {
  machine: Machine;
  token: string | null;
  onChanged: () => void;
}

function lastSeen(machine: Machine): string {
  if (machine.online) return "online now";
  if (!machine.last_seen_at) return "never connected";
  const then = new Date(machine.last_seen_at).getTime();
  const mins = Math.round((Date.now() - then) / 60000);
  if (mins < 1) return "seen just now";
  if (mins < 60) return `seen ${mins}m ago`;
  const hrs = Math.round(mins / 60);
  if (hrs < 24) return `seen ${hrs}h ago`;
  return `seen ${Math.round(hrs / 24)}d ago`;
}

export function MachineCard({ machine, token, onChanged }: MachineCardProps) {
  const navigate = useNavigate();
  const [showDelete, setShowDelete] = useState(false);

  const handleConnect = () => {
    if (machine.online) navigate(`/machine/${machine.id}`);
  };

  const handleDelete = async () => {
    try {
      await deleteMachine(machine.id, token);
    } catch {
      // May already be gone.
    }
    onChanged();
  };

  return (
    <>
      <div className="card-crt" style={{ display: "flex" }}>
        <div
          onClick={handleConnect}
          style={{
            flex: 1,
            cursor: machine.online ? "pointer" : "default",
            opacity: machine.online ? 1 : 0.6,
          }}
        >
          <div style={{ display: "flex", alignItems: "center", gap: 10, marginBottom: 6 }}>
            <span
              style={{
                color: "var(--green)",
                fontWeight: 600,
                fontSize: 13,
                textShadow: "0 0 6px rgba(0,255,65,0.3)",
              }}
            >
              {machine.name}
            </span>
            {machine.online ? (
              <span className="badge badge-green">[online]</span>
            ) : (
              <span className="badge badge-red">[offline]</span>
            )}
          </div>
          <div style={{ display: "flex", gap: 12, fontSize: 11, color: "var(--text-dim)" }}>
            {machine.hostname && <span>{machine.hostname}</span>}
            <span>{lastSeen(machine)}</span>
          </div>
        </div>
        <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
          {machine.online && (
            <button className="btn-action" onClick={handleConnect}>
              [connect]
            </button>
          )}
          <button className="btn-danger" onClick={() => setShowDelete(true)}>
            [remove]
          </button>
        </div>
      </div>

      {showDelete && (
        <div className="modal-overlay" onClick={() => setShowDelete(false)}>
          <div
            className="modal-panel"
            onClick={(e) => e.stopPropagation()}
            style={{ maxWidth: 420 }}
          >
            <div style={{ color: "var(--red)", fontWeight: "bold", fontSize: 14 }}>
              // REMOVE MACHINE
            </div>
            <div style={{ color: "var(--text)", fontSize: 13, lineHeight: 1.5 }}>
              This unenrolls <strong>{machine.name}</strong> and closes any active tunnel.
              To reconnect it later you must run{" "}
              <code style={{ color: "var(--green)" }}>phosphor enroll</code> on that machine again.
            </div>
            <div style={{ display: "flex", gap: 12, justifyContent: "flex-end" }}>
              <button className="btn-action" onClick={() => setShowDelete(false)}>
                [cancel]
              </button>
              <button className="btn-danger" onClick={handleDelete}>
                [remove machine]
              </button>
            </div>
          </div>
        </div>
      )}
    </>
  );
}
