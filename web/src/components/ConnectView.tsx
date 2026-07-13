import { useCallback, useEffect, useRef, useState } from "react";
import { useParams, Link } from "react-router";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { WebglAddon } from "@xterm/addon-webgl";
import "@xterm/xterm/css/xterm.css";
import { useAuth } from "../auth/useAuth";
import { useSSH } from "../hooks/useSSH";
import { listKeys, type StoredKey } from "../lib/keys";
import { AuthModal } from "./AuthModal";

export function ConnectView() {
  const { id } = useParams<{ id: string }>();
  const { getToken } = useAuth();
  const containerRef = useRef<HTMLDivElement>(null);
  const termRef = useRef<Terminal | null>(null);
  const fitRef = useRef<FitAddon | null>(null);

  // Connection form state (before the session starts).
  const [username, setUsername] = useState("");
  const [selectedKeyId, setSelectedKeyId] = useState<string>("");
  const [keys, setKeys] = useState<StoredKey[]>([]);
  const [started, setStarted] = useState(false);

  useEffect(() => {
    void listKeys().then(setKeys);
    // Prefill the last username used for this machine.
    const remembered = localStorage.getItem(`phosphor_user_${id}`);
    if (remembered) setUsername(remembered);
  }, [id]);

  const onData = useCallback((data: Uint8Array) => {
    termRef.current?.write(data);
  }, []);

  const onClose = useCallback(() => {
    termRef.current?.write("\r\n\x1b[1;31m[Session closed]\x1b[0m\r\n");
  }, []);

  const selectedKey = keys.find((k) => k.id === selectedKeyId) ?? null;

  const { connected, connecting, error, authRequest, sendStdin, sendResize, disconnect } = useSSH({
    machineId: id ?? "",
    token: getToken(),
    username,
    key: selectedKey,
    connect: started,
    onData,
    onClose,
  });

  // Initialize terminal once the session starts.
  useEffect(() => {
    if (!started || !containerRef.current) return;

    const term = new Terminal({
      fontFamily: '"Fira Code", "Cascadia Code", monospace',
      fontSize: 14,
      theme: {
        background: "#050808",
        foreground: "#b0b0b0",
        cursor: "#00ff41",
        cursorAccent: "#050808",
        selectionBackground: "rgba(0, 255, 65, 0.2)",
      },
      cursorBlink: true,
      allowProposedApi: true,
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.open(containerRef.current);
    term.attachCustomKeyEventHandler((event) => {
      if (event.key === "Escape") event.preventDefault();
      return true;
    });
    try {
      term.loadAddon(new WebglAddon());
    } catch {
      // Canvas fallback is fine.
    }
    fit.fit();
    termRef.current = term;
    fitRef.current = fit;

    const ro = new ResizeObserver(() => fit.fit());
    ro.observe(containerRef.current);

    return () => {
      ro.disconnect();
      term.dispose();
      termRef.current = null;
      fitRef.current = null;
    };
  }, [started]);

  // Wire input and resize once connected.
  useEffect(() => {
    const term = termRef.current;
    if (!term || !connected) return;

    const dataDisp = term.onData((data) => sendStdin(data));
    const resizeDisp = term.onResize(({ cols, rows }) => sendResize(cols, rows));
    fitRef.current?.fit();
    sendResize(term.cols, term.rows);

    return () => {
      dataDisp.dispose();
      resizeDisp.dispose();
    };
  }, [connected, sendStdin, sendResize]);

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!username) return;
    localStorage.setItem(`phosphor_user_${id}`, username);
    setStarted(true);
  };

  if (!started) {
    return (
      <div style={{ display: "flex", flexDirection: "column", height: "100%", gap: 16 }}>
        <div className="status-bar">
          <Link to="/" style={{ color: "#00aa33", textDecoration: "none" }}>
            &larr; machines
          </Link>
        </div>
        <form
          onSubmit={handleSubmit}
          style={{
            maxWidth: 420,
            margin: "40px auto",
            display: "flex",
            flexDirection: "column",
            gap: 16,
            width: "100%",
          }}
        >
          <div style={{ color: "var(--cyan, #00e5ff)", fontWeight: "bold", fontSize: 14 }}>
            // CONNECT
          </div>
          <label style={{ display: "flex", flexDirection: "column", gap: 6, fontSize: 12, color: "var(--text-dim)" }}>
            SSH username
            <input
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              placeholder="e.g. ubuntu"
              autoFocus
              style={{
                background: "#0a0a0a",
                border: "1px solid var(--border-crt)",
                color: "var(--green)",
                padding: "6px 10px",
                fontFamily: "inherit",
                fontSize: 13,
                outline: "none",
              }}
            />
          </label>
          <label style={{ display: "flex", flexDirection: "column", gap: 6, fontSize: 12, color: "var(--text-dim)" }}>
            Key (optional — password prompt if none)
            <select
              value={selectedKeyId}
              onChange={(e) => setSelectedKeyId(e.target.value)}
              style={{
                background: "#0a0a0a",
                border: "1px solid var(--border-crt)",
                color: "var(--green)",
                padding: "6px 10px",
                fontFamily: "inherit",
                fontSize: 13,
              }}
            >
              <option value="">Password authentication</option>
              {keys.map((k) => (
                <option key={k.id} value={k.id}>
                  {k.name} ({k.fingerprint.slice(0, 24)}…)
                </option>
              ))}
            </select>
          </label>
          <button type="submit" className="btn-action" disabled={!username}>
            [connect]
          </button>
          <div style={{ fontSize: 11, color: "var(--text-dim)" }}>
            Manage keys on the <Link to="/keys" style={{ color: "var(--green)" }}>keys page</Link>.
          </div>
        </form>
      </div>
    );
  }

  return (
    <div style={{ display: "flex", flexDirection: "column", height: "100%", gap: 8 }}>
      <div className="status-bar">
        <div style={{ display: "flex", gap: 10, alignItems: "center" }}>
          <Link to="/" style={{ color: "#00aa33", textDecoration: "none" }}>
            &larr; machines
          </Link>
          <span style={{ color: "var(--amber)" }}>{username}</span>
        </div>
        <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
          <span className="badge badge-cyan">[e2e ssh]</span>
          {connected ? (
            <span className="badge badge-green">[connected]</span>
          ) : error ? (
            <span className="badge badge-red">[{error}]</span>
          ) : connecting ? (
            <span className="badge badge-amber">[connecting...]</span>
          ) : (
            <span className="badge badge-red">[disconnected]</span>
          )}
          <button className="btn-danger" onClick={disconnect}>
            [disconnect]
          </button>
        </div>
      </div>

      {authRequest && <AuthModal request={authRequest} />}

      <div
        ref={containerRef}
        style={{
          flex: 1,
          border: "1px solid var(--border-crt)",
          background: "#050808",
          overflow: "hidden",
        }}
      />
    </div>
  );
}
