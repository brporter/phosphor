import { useAuth } from "../auth/useAuth";
import { useMachines } from "../hooks/useMachines";
import { MachineCard } from "./MachineCard";

export function MachineList() {
  const { getToken } = useAuth();
  const { machines, isLoading, error, refresh } = useMachines();

  if (isLoading) {
    return (
      <div style={{ color: "var(--green)", textShadow: "0 0 6px rgba(0,255,65,0.4)", padding: 16 }}>
        Loading machines...
      </div>
    );
  }

  if (error) {
    return (
      <div style={{ color: "var(--red)", textShadow: "0 0 6px rgba(255,51,51,0.4)", padding: 16 }}>
        Error: {error}
      </div>
    );
  }

  if (machines.length === 0) {
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
        <pre style={{ color: "var(--green-dim)", fontSize: 13, textShadow: "0 0 6px rgba(0,255,65,0.4)" }}>
          {`$ phosphor enroll --relay ${window.location.origin}
Machine enrolled.
$ phosphor tunnel`}
        </pre>
        <p style={{ fontSize: 13 }}>No machines yet. Enroll one from your terminal:</p>
        <code
          style={{
            background: "var(--bg-card-crt)",
            border: "1px solid var(--border-crt)",
            padding: "8px 16px",
            color: "var(--amber)",
          }}
        >
          phosphor enroll --relay {window.location.origin}
        </code>
      </div>
    );
  }

  return (
    <div>
      <div className="section-heading" style={{ marginBottom: 16 }}>
        // MACHINES =============== [{machines.length}]
      </div>
      <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
        {machines.map((m) => (
          <MachineCard key={m.id} machine={m} token={getToken()} onChanged={refresh} />
        ))}
      </div>
    </div>
  );
}
