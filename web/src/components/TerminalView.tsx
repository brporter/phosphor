import { useCallback, useEffect, useRef, useState } from "react";
import { useParams, Link } from "react-router";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { WebglAddon } from "@xterm/addon-webgl";
import "@xterm/xterm/css/xterm.css";
import { useAuth } from "../auth/useAuth";
import { useWebSocket, type FileTransfer } from "../hooks/useWebSocket";

export function TerminalView() {
  const { id } = useParams<{ id: string }>();
  const { getToken } = useAuth();
  const containerRef = useRef<HTMLDivElement>(null);
  const termRef = useRef<Terminal | null>(null);
  const fitRef = useRef<FitAddon | null>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const [ended, setEnded] = useState(false);
  const [dragActive, setDragActive] = useState(false);

  const onData = useCallback((data: Uint8Array) => {
    termRef.current?.write(data);
  }, []);

  const onResize = useCallback((cols: number, rows: number) => {
    termRef.current?.resize(cols, rows);
  }, []);

  const onEnd = useCallback(() => {
    setEnded(true);
    termRef.current?.write("\r\n\x1b[1;31m[Session ended]\x1b[0m\r\n");
  }, []);

  const [keyInput, setKeyInput] = useState("");

  const { connected, joined, error, processExited, fileTransfers, sendStdin, sendResize, sendRestart, sendFile, needsKey, decryptionError, setEncryptionPassphrase } = useWebSocket({
    sessionId: id ?? "",
    token: getToken(),
    onData,
    onResize,
    onEnd,
  });

  // Initialize terminal
  useEffect(() => {
    if (!containerRef.current) return;

    const term = new Terminal({
      fontFamily: '"Fira Code", "Cascadia Code", monospace',
      fontSize: 14,
      theme: {
        background: "#050808",
        foreground: "#b0b0b0",
        cursor: "#00ff41",
        cursorAccent: "#050808",
        selectionBackground: "rgba(0, 255, 65, 0.2)",
        black: "#0a0a0a",
        red: "#ff3333",
        green: "#00ff41",
        yellow: "#ffb000",
        blue: "#0088ff",
        magenta: "#cc00ff",
        cyan: "#00e5ff",
        white: "#b0b0b0",
        brightBlack: "#555555",
        brightRed: "#ff6666",
        brightGreen: "#66ff66",
        brightYellow: "#ffcc33",
        brightBlue: "#3399ff",
        brightMagenta: "#dd66ff",
        brightCyan: "#66ffff",
        brightWhite: "#ffffff",
      },
      cursorBlink: true,
      allowProposedApi: true,
    });

    const fit = new FitAddon();
    term.loadAddon(fit);

    term.open(containerRef.current);

    // Prevent browser from swallowing Escape (it would blur the hidden textarea)
    term.attachCustomKeyEventHandler((event) => {
      if (event.key === "Escape") {
        event.preventDefault();
      }
      return true;
    });

    // Try WebGL renderer
    try {
      term.loadAddon(new WebglAddon());
    } catch {
      // Canvas fallback is fine
    }

    fit.fit();
    termRef.current = term;
    fitRef.current = fit;

    const resizeObserver = new ResizeObserver(() => {
      fit.fit();
    });
    resizeObserver.observe(containerRef.current);

    return () => {
      resizeObserver.disconnect();
      term.dispose();
      termRef.current = null;
      fitRef.current = null;
    };
  }, []);

  // Wire terminal input to WebSocket
  useEffect(() => {
    const term = termRef.current;
    if (!term || !joined) return;

    // Forward user input
    const disposable = term.onData((data) => {
      if (joined.mode === "pty") {
        const encoded = new TextEncoder().encode(data);
        sendStdin(encoded);
      }
    });

    return () => disposable.dispose();
  }, [joined, sendStdin]);

  // Forward resize events
  useEffect(() => {
    const term = termRef.current;
    if (!term || !joined) return;

    const disposable = term.onResize(({ cols, rows }) => {
      sendResize(cols, rows);
    });

    // Fit to container and send current dimensions to relay
    fitRef.current?.fit();
    sendResize(term.cols, term.rows);

    return () => disposable.dispose();
  }, [joined, sendResize]);

  useEffect(() => {
    if (processExited !== null) {
      termRef.current?.write(
        `\r\n\x1b[1;33m[Process exited (code ${processExited})]\x1b[0m\r\n`
      );
    }
  }, [processExited]);

  // File upload handlers
  const openFilePicker = useCallback(() => {
    fileInputRef.current?.click();
  }, []);

  const handleFileSelected = useCallback(
    (e: React.ChangeEvent<HTMLInputElement>) => {
      const file = e.target.files?.[0];
      if (file) {
        sendFile(file);
      }
      // Reset so the same file can be re-selected
      e.target.value = "";
    },
    [sendFile]
  );

  const handleDragOver = useCallback((e: React.DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
    setDragActive(true);
  }, []);

  const handleDragLeave = useCallback((e: React.DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
    setDragActive(false);
  }, []);

  const handleDrop = useCallback(
    (e: React.DragEvent) => {
      e.preventDefault();
      e.stopPropagation();
      setDragActive(false);
      const file = e.dataTransfer.files[0];
      if (file) {
        sendFile(file);
      }
    },
    [sendFile]
  );

  // Collect active uploads
  const activeUploads: FileTransfer[] = [];
  fileTransfers.forEach((t) => {
    if (t.status === "uploading" || t.status === "error") {
      activeUploads.push(t);
    }
  });

  const showUploadButton = connected && joined?.mode === "pty";

  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        height: "100%",
        gap: 8,
      }}
    >
      {/* Status bar */}
      <div className="status-bar">
        <div style={{ display: "flex", gap: 10, alignItems: "center" }}>
          <Link to="/" style={{ color: "#00aa33", textDecoration: "none" }}>
            &larr; sessions
          </Link>
          {joined && (
            <span
              style={{
                color: "var(--amber)",
                textShadow: "0 0 4px rgba(255,176,0,0.3)",
              }}
            >
              {joined.command || joined.mode}
            </span>
          )}
        </div>
        <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
          {showUploadButton && (
            <>
              <button className="btn-action" onClick={openFilePicker}>[upload]</button>
              <input
                type="file"
                ref={fileInputRef}
                style={{ display: "none" }}
                onChange={handleFileSelected}
              />
            </>
          )}
          {joined && (
            joined.encrypted ? (
              <span className="badge badge-cyan">[encrypted]</span>
            ) : (
              <span className="badge badge-amber">[unencrypted]</span>
            )
          )}
          {processExited !== null ? (
            <>
              <span className="badge badge-amber">[exited ({processExited})]</span>
              <button className="btn-action" onClick={sendRestart}>[restart]</button>
            </>
          ) : ended ? (
            <span className="badge badge-red">[disconnected]</span>
          ) : connected ? (
            <span className="badge badge-green">[connected]</span>
          ) : error ? (
            <span className="badge badge-red">[{error}]</span>
          ) : (
            <span className="badge badge-amber">[connecting...]</span>
          )}
        </div>
      </div>

      {/* Upload progress bar */}
      {activeUploads.length > 0 && (
        <div className="upload-progress-bar">
          {activeUploads.map((t) => {
            const pct = t.size > 0 ? Math.round((t.bytesWritten / t.size) * 100) : 0;
            return (
              <div key={t.id} className="upload-progress-item">
                <span className="upload-progress-name">{t.name}</span>
                {t.status === "error" ? (
                  <span className="upload-progress-error">{t.error}</span>
                ) : (
                  <>
                    <div className="progress-track">
                      <div className="progress-fill" style={{ width: `${pct}%` }} />
                    </div>
                    <span className="upload-progress-pct">{pct}%</span>
                  </>
                )}
              </div>
            );
          })}
        </div>
      )}

      {/* Passphrase modal for encrypted sessions */}
      {needsKey && (
        <div
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
                color: "var(--cyan, #00e5ff)",
                fontWeight: "bold",
                fontSize: 14,
                textShadow: "0 0 6px rgba(0, 229, 255, 0.4)",
              }}
            >
              // ENCRYPTED SESSION
            </div>
            <div style={{ color: "var(--text)", fontSize: 13, lineHeight: 1.5 }}>
              This session is encrypted. Enter the passphrase to decrypt.
            </div>
            {decryptionError && (
              <div style={{ color: "var(--red)", fontSize: 12 }}>
                {decryptionError}
              </div>
            )}
            <form
              onSubmit={(e) => {
                e.preventDefault();
                if (keyInput) {
                  setEncryptionPassphrase(keyInput);
                }
              }}
              style={{ display: "flex", gap: 8 }}
            >
              <input
                type="password"
                value={keyInput}
                onChange={(e) => setKeyInput(e.target.value)}
                placeholder="Passphrase"
                autoFocus
                style={{
                  flex: 1,
                  background: "#0a0a0a",
                  border: "1px solid var(--border-crt)",
                  color: "var(--green)",
                  padding: "6px 10px",
                  fontFamily: "inherit",
                  fontSize: 13,
                  outline: "none",
                }}
              />
              <button type="submit" className="btn-action">
                [decrypt]
              </button>
            </form>
          </div>
        </div>
      )}

      {/* Terminal container with drag-and-drop */}
      <div
        ref={containerRef}
        style={{
          flex: 1,
          border: "1px solid var(--border-crt)",
          background: "#050808",
          overflow: "hidden",
          position: "relative",
        }}
        onDragOver={showUploadButton ? handleDragOver : undefined}
        onDragEnter={showUploadButton ? handleDragOver : undefined}
        onDragLeave={showUploadButton ? handleDragLeave : undefined}
        onDrop={showUploadButton ? handleDrop : undefined}
      >
        {dragActive && (
          <div className="drop-overlay">
            <span>Drop file to upload</span>
          </div>
        )}
      </div>

      {/* Mode indicator */}
      {joined?.mode === "pipe" && (
        <div style={{ fontSize: 11, color: "var(--text-dim)", textAlign: "center" }}>
          view-only (pipe mode)
        </div>
      )}

    </div>
  );
}
