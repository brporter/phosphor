import { useCallback, useEffect, useRef, useState } from "react";
import { loadSSH, type SSHHandle } from "../lib/wasm";
import { getHostKeyPin, setHostKeyPin } from "../lib/keys";
import type { StoredKey } from "../lib/keys";

// AuthRequest drives a modal in the UI; respond() resolves the pending WASM
// callback.
export type AuthRequest =
  | { kind: "password"; respond: (value: string | null) => void }
  | {
      kind: "keyboard-interactive";
      name: string;
      instruction: string;
      questions: string[];
      respond: (answers: string[] | null) => void;
    }
  | {
      kind: "hostkey";
      fingerprint: string;
      keyType: string;
      known: boolean;
      respond: (accept: boolean) => void;
    }
  | { kind: "key-passphrase"; respond: (value: string | null) => void };

export interface UseSSHOptions {
  machineId: string;
  token: string | null;
  username: string;
  // A selected key for public-key auth (optional; password auth if omitted).
  key?: StoredKey | null;
  connect: boolean;
  onData: (data: Uint8Array) => void;
  onClose?: () => void;
}

export interface UseSSHResult {
  connected: boolean;
  connecting: boolean;
  error: string | null;
  authRequest: AuthRequest | null;
  sendStdin: (data: Uint8Array | string) => void;
  sendResize: (cols: number, rows: number) => void;
  disconnect: () => void;
}

export function useSSH(opts: UseSSHOptions): UseSSHResult {
  const { machineId, token, username, key, connect, onData, onClose } = opts;
  const [connected, setConnected] = useState(false);
  const [connecting, setConnecting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [authRequest, setAuthRequest] = useState<AuthRequest | null>(null);
  const handleRef = useRef<SSHHandle | null>(null);
  const startedRef = useRef(false);

  // Stable refs for callbacks used deep inside the async connect.
  const onDataRef = useRef(onData);
  onDataRef.current = onData;
  const onCloseRef = useRef(onClose);
  onCloseRef.current = onClose;

  const disconnect = useCallback(() => {
    handleRef.current?.disconnect();
    handleRef.current = null;
    setConnected(false);
  }, []);

  useEffect(() => {
    if (!connect || !machineId || !username || startedRef.current) return;
    startedRef.current = true;
    setConnecting(true);
    let cancelled = false;

    const wsProto = window.location.protocol === "https:" ? "wss:" : "ws:";
    const wsURL = `${wsProto}//${window.location.host}/ws/ssh/${machineId}`;

    void (async () => {
      try {
        const ssh = await loadSSH();
        const handle = await ssh.connect({
          wsURL,
          token,
          username,
          privateKey: key?.privateKeyPem,
          rows: 24,
          cols: 80,
          callbacks: {
            onData: (d) => onDataRef.current(d),
            onClose: () => {
              if (cancelled) return;
              setConnected(false);
              onCloseRef.current?.();
            },
            onPassword: () =>
              new Promise<string>((resolve, reject) => {
                setAuthRequest({
                  kind: "password",
                  respond: (value) => {
                    setAuthRequest(null);
                    if (value === null) reject(new Error("cancelled"));
                    else resolve(value);
                  },
                });
              }),
            onKeyboardInteractive: (name, instruction, questions) =>
              new Promise<string[]>((resolve, reject) => {
                setAuthRequest({
                  kind: "keyboard-interactive",
                  name,
                  instruction,
                  questions,
                  respond: (answers) => {
                    setAuthRequest(null);
                    if (answers === null) reject(new Error("cancelled"));
                    else resolve(answers);
                  },
                });
              }),
            onHostKey: (fingerprint, keyType) =>
              new Promise<boolean>((resolve) => {
                const pinned = getHostKeyPin(machineId);
                if (pinned && pinned === fingerprint) {
                  resolve(true);
                  return;
                }
                setAuthRequest({
                  kind: "hostkey",
                  fingerprint,
                  keyType,
                  known: pinned !== null,
                  respond: (accept) => {
                    setAuthRequest(null);
                    if (accept) setHostKeyPin(machineId, fingerprint);
                    resolve(accept);
                  },
                });
              }),
          },
        });
        if (cancelled) {
          handle.disconnect();
          return;
        }
        handleRef.current = handle;
        setConnected(true);
        setConnecting(false);
      } catch (err) {
        if (cancelled) return;
        setError(err instanceof Error ? err.message : "connection failed");
        setConnecting(false);
      }
    })();

    return () => {
      cancelled = true;
      handleRef.current?.disconnect();
      handleRef.current = null;
    };
  }, [connect, machineId, username, token, key]);

  const sendStdin = useCallback((data: Uint8Array | string) => {
    handleRef.current?.write(data);
  }, []);

  const sendResize = useCallback((cols: number, rows: number) => {
    handleRef.current?.resize(cols, rows);
  }, []);

  return { connected, connecting, error, authRequest, sendStdin, sendResize, disconnect };
}
