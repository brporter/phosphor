import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useParams, Link } from "react-router";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { WebglAddon } from "@xterm/addon-webgl";
import "@xterm/xterm/css/xterm.css";
import { useAuth } from "../auth/useAuth";
import { useSSH, type SessionKey } from "../hooks/useSSH";
import { listKeys, type StoredKey } from "../lib/keys";
import { loadSSH } from "../lib/wasm";
import { AuthModal } from "./AuthModal";

// Sentinel <select> value for a key pasted/loaded just for this session.
const INLINE_KEY = "__inline__";

export function ConnectView() {
  const { id } = useParams<{ id: string }>();
  const { getToken } = useAuth();
  const containerRef = useRef<HTMLDivElement>(null);
  const termRef = useRef<Terminal | null>(null);
  const fitRef = useRef<FitAddon | null>(null);
  const fileRef = useRef<HTMLInputElement>(null);

  // Connection form state (before the session starts).
  const [username, setUsername] = useState("");
  const [selectedKeyId, setSelectedKeyId] = useState<string>("");
  const [keys, setKeys] = useState<StoredKey[]>([]);
  // On-demand key: lives only in component state, never persisted.
  const [inlinePem, setInlinePem] = useState("");
  const [inlinePassphrase, setInlinePassphrase] = useState("");
  const [formError, setFormError] = useState<string | null>(null);
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

  // Memoized so its identity is stable across renders — useSSH tears down the
  // session when the key reference changes.
  const sessionKey = useMemo<SessionKey | null>(() => {
    if (selectedKeyId === INLINE_KEY) {
      if (!inlinePem.trim()) return null;
      return { privateKeyPem: inlinePem, passphrase: inlinePassphrase || undefined };
    }
    const stored = keys.find((k) => k.id === selectedKeyId);
    return stored ? { privateKeyPem: stored.privateKeyPem } : null;
  }, [selectedKeyId, keys, inlinePem, inlinePassphrase]);

  const { connected, connecting, error, authRequest, sendStdin, sendResize, disconnect } = useSSH({
    machineId: id ?? "",
    token: getToken(),
    username,
    key: sessionKey,
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

  const usingInlineKey = selectedKeyId === INLINE_KEY;
  const canConnect = username !== "" && (!usingInlineKey || inlinePem.trim() !== "");

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!canConnect) return;
    setFormError(null);
    void (async () => {
      if (usingInlineKey) {
        // Validate the pasted key (and passphrase) before opening the session.
        try {
          const ssh = await loadSSH();
          ssh.publicKeyFromPem(inlinePem, inlinePassphrase || undefined);
        } catch (err) {
          setFormError(err instanceof Error ? err.message : "invalid private key");
          return;
        }
      }
      localStorage.setItem(`phosphor_user_${id}`, username);
      setStarted(true);
    })();
  };

  const handleKeyFile = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    e.target.value = "";
    if (!file) return;
    setFormError(null);
    setInlinePem(await file.text());
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
              className="input-crt"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              placeholder="e.g. ubuntu"
              autoFocus
            />
          </label>
          <label style={{ display: "flex", flexDirection: "column", gap: 6, fontSize: 12, color: "var(--text-dim)" }}>
            Key (optional — password prompt if none)
            <select
              className="input-crt"
              value={selectedKeyId}
              onChange={(e) => {
                setSelectedKeyId(e.target.value);
                setFormError(null);
              }}
            >
              <option value="">Password authentication</option>
              {keys.map((k) => (
                <option key={k.id} value={k.id}>
                  {k.name} ({k.fingerprint.slice(0, 24)}…)
                </option>
              ))}
              <option value={INLINE_KEY}>Use a key just for this session…</option>
            </select>
          </label>
          {usingInlineKey && (
            <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
              <label style={{ display: "flex", flexDirection: "column", gap: 6, fontSize: 12, color: "var(--text-dim)" }}>
                Private key (PEM) — used for this session only, never stored
                <textarea
                  className="input-crt"
                  value={inlinePem}
                  onChange={(e) => {
                    setInlinePem(e.target.value);
                    setFormError(null);
                  }}
                  placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
                  rows={5}
                  style={{ resize: "vertical" }}
                />
              </label>
              <div>
                <input
                  ref={fileRef}
                  type="file"
                  style={{ display: "none" }}
                  onChange={(e) => void handleKeyFile(e)}
                />
                <button type="button" className="btn-action" onClick={() => fileRef.current?.click()}>
                  [load from file]
                </button>
              </div>
              <label style={{ display: "flex", flexDirection: "column", gap: 6, fontSize: 12, color: "var(--text-dim)" }}>
                Key passphrase (only if the key is encrypted)
                <input
                  className="input-crt"
                  type="password"
                  value={inlinePassphrase}
                  onChange={(e) => {
                    setInlinePassphrase(e.target.value);
                    setFormError(null);
                  }}
                />
              </label>
            </div>
          )}
          {formError && <div style={{ color: "var(--red)", fontSize: 12 }}>{formError}</div>}
          <button type="submit" className="btn-action" disabled={!canConnect}>
            [connect]
          </button>
          <div style={{ fontSize: 11, color: "var(--text-dim)" }}>
            Manage stored keys on the <Link to="/keys" style={{ color: "var(--green)" }}>keys page</Link>.
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
