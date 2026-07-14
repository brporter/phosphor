import { useState } from "react";
import type { AuthRequest } from "../hooks/useSSH";

export function AuthModal({ request }: { request: AuthRequest }) {
  switch (request.kind) {
    case "password":
    case "key-passphrase":
      return (
        <PromptModal
          title={request.kind === "password" ? "// PASSWORD" : "// KEY PASSPHRASE"}
          label={request.kind === "password" ? "Enter your SSH password" : "Enter the key passphrase"}
          onSubmit={(v) => request.respond(v)}
          onCancel={() => request.respond(null)}
        />
      );
    case "keyboard-interactive":
      return <KeyboardInteractiveModal request={request} />;
    case "hostkey":
      return <HostKeyModal request={request} />;
  }
}

function PromptModal({
  title,
  label,
  onSubmit,
  onCancel,
}: {
  title: string;
  label: string;
  onSubmit: (value: string) => void;
  onCancel: () => void;
}) {
  const [value, setValue] = useState("");
  return (
    <div className="modal-overlay">
      <form
        className="modal-panel"
        onSubmit={(e) => {
          e.preventDefault();
          onSubmit(value);
        }}
      >
        <div style={{ color: "var(--cyan, #00e5ff)", fontWeight: "bold", fontSize: 14 }}>{title}</div>
        <div style={{ color: "var(--text)", fontSize: 13 }}>{label}</div>
        <input
          type="password"
          value={value}
          onChange={(e) => setValue(e.target.value)}
          autoFocus
          className="input-crt"
        />
        <div style={{ display: "flex", gap: 12, justifyContent: "flex-end" }}>
          <button type="button" className="btn-action" onClick={onCancel}>
            [cancel]
          </button>
          <button type="submit" className="btn-action">
            [submit]
          </button>
        </div>
      </form>
    </div>
  );
}

function KeyboardInteractiveModal({
  request,
}: {
  request: Extract<AuthRequest, { kind: "keyboard-interactive" }>;
}) {
  const [answers, setAnswers] = useState<string[]>(request.questions.map(() => ""));
  return (
    <div className="modal-overlay">
      <form
        className="modal-panel"
        onSubmit={(e) => {
          e.preventDefault();
          request.respond(answers);
        }}
      >
        <div style={{ color: "var(--cyan, #00e5ff)", fontWeight: "bold", fontSize: 14 }}>
          // {request.name || "AUTHENTICATION"}
        </div>
        {request.instruction && (
          <div style={{ color: "var(--text)", fontSize: 13 }}>{request.instruction}</div>
        )}
        {request.questions.map((q, i) => (
          <label key={i} style={{ display: "flex", flexDirection: "column", gap: 6, fontSize: 12, color: "var(--text-dim)" }}>
            {q}
            <input
              type="password"
              value={answers[i]}
              autoFocus={i === 0}
              onChange={(e) => {
                const next = [...answers];
                next[i] = e.target.value;
                setAnswers(next);
              }}
              className="input-crt"
            />
          </label>
        ))}
        <div style={{ display: "flex", gap: 12, justifyContent: "flex-end" }}>
          <button type="button" className="btn-action" onClick={() => request.respond(null)}>
            [cancel]
          </button>
          <button type="submit" className="btn-action">
            [submit]
          </button>
        </div>
      </form>
    </div>
  );
}

function HostKeyModal({ request }: { request: Extract<AuthRequest, { kind: "hostkey" }> }) {
  return (
    <div className="modal-overlay">
      <div className="modal-panel">
        <div
          style={{
            color: request.known ? "var(--red)" : "var(--amber)",
            fontWeight: "bold",
            fontSize: 14,
          }}
        >
          // {request.known ? "HOST KEY CHANGED" : "VERIFY HOST KEY"}
        </div>
        {request.known ? (
          <div style={{ color: "var(--red)", fontSize: 13, lineHeight: 1.5 }}>
            <strong>Warning:</strong> this machine's host key does not match the one you
            previously trusted. This could indicate a man-in-the-middle attack, or the host
            may have been reinstalled. Do not continue unless you know why it changed.
          </div>
        ) : (
          <div style={{ color: "var(--text)", fontSize: 13, lineHeight: 1.5 }}>
            You are connecting to this machine for the first time. Verify the host key
            fingerprint matches what you expect before trusting it.
          </div>
        )}
        <div style={{ fontSize: 12, color: "var(--text-dim)" }}>
          <div>type: {request.keyType}</div>
          <div style={{ color: "var(--green)", wordBreak: "break-all" }}>{request.fingerprint}</div>
        </div>
        <div style={{ display: "flex", gap: 12, justifyContent: "flex-end" }}>
          <button className="btn-action" onClick={() => request.respond(false)}>
            [cancel]
          </button>
          <button
            className={request.known ? "btn-danger" : "btn-action"}
            onClick={() => request.respond(true)}
          >
            [trust &amp; continue]
          </button>
        </div>
      </div>
    </div>
  );
}
