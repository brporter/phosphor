import { useCallback, useEffect, useRef, useState } from "react";
import { useParams, Link } from "react-router";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { WebglAddon } from "@xterm/addon-webgl";
import "@xterm/xterm/css/xterm.css";
import { useAuth } from "../auth/useAuth";
import { useWebSocket } from "../hooks/useWebSocket";

export function TerminalView() {
  const { id } = useParams<{ id: string }>();
  const { getToken } = useAuth();
  const containerRef = useRef<HTMLDivElement>(null);
  const termRef = useRef<Terminal | null>(null);
  const fitRef = useRef<FitAddon | null>(null);
  const [ended, setEnded] = useState(false);

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

  const { connected, joined, error, sendStdin, sendResize } = useWebSocket({
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
        background: "#0a0a0a",
        foreground: "#b0b0b0",
        cursor: "#00ff41",
        cursorAccent: "#0a0a0a",
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

    // Resize terminal to match session dimensions
    if (joined.cols > 0 && joined.rows > 0) {
      term.resize(joined.cols, joined.rows);
    }

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

    return () => disposable.dispose();
  }, [joined, sendResize]);

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
      <div
        style={{
          display: "flex",
          justifyContent: "space-between",
          alignItems: "center",
          fontSize: 12,
        }}
      >
        <div style={{ display: "flex", gap: 12, alignItems: "center" }}>
          <Link to="/" style={{ color: "var(--text)" }}>
            &larr; sessions
          </Link>
          {joined && (
            <span style={{ color: "var(--amber)" }}>
              {joined.command || joined.mode} ({joined.cols}x{joined.rows})
            </span>
          )}
        </div>
        <div>
          {ended ? (
            <span style={{ color: "var(--red)" }}>disconnected</span>
          ) : connected ? (
            <span style={{ color: "var(--green)" }}>connected</span>
          ) : error ? (
            <span style={{ color: "var(--red)" }}>{error}</span>
          ) : (
            <span style={{ color: "var(--amber)" }}>connecting...</span>
          )}
        </div>
      </div>

      {/* Terminal container */}
      <div
        ref={containerRef}
        style={{
          flex: 1,
          border: "1px solid var(--border)",
          background: "#0a0a0a",
          overflow: "hidden",
        }}
      />

      {/* Mode indicator */}
      {joined?.mode === "pipe" && (
        <div style={{ fontSize: 11, color: "var(--text)", textAlign: "center" }}>
          view-only (pipe mode)
        </div>
      )}
    </div>
  );
}
