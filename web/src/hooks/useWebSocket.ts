import { useCallback, useEffect, useRef, useState } from "react";
import {
  MsgType,
  decode,
  decodeJSON,
  encode,
  type JoinedPayload,
  type ErrorPayload,
  type ReconnectPayload,
  type ProcessExitedPayload,
  type FileAckPayload,
} from "../lib/protocol";

const FILE_CHUNK_SIZE = 32 * 1024; // 32KB
const TRANSFER_ID_LEN = 8;
const ACK_TIMEOUT_MS = 30_000; // 30s timeout for ack
const COMPLETED_TRANSFER_TTL_MS = 10_000; // remove completed/errored transfers after 10s

export interface FileTransfer {
  id: string;
  name: string;
  size: number;
  bytesWritten: number;
  status: "uploading" | "complete" | "error";
  error?: string;
}

interface UseWebSocketOptions {
  sessionId: string;
  token: string | null;
  onData: (data: Uint8Array) => void;
  onResize: (cols: number, rows: number) => void;
  onEnd: () => void;
}

function generateTransferID(): string {
  const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789";
  const arr = new Uint8Array(TRANSFER_ID_LEN);
  crypto.getRandomValues(arr);
  // Rejection sampling to avoid modulo bias (256 % 62 != 0)
  const limit = 256 - (256 % chars.length); // 252
  let id = "";
  let i = 0;
  while (id.length < TRANSFER_ID_LEN) {
    if (arr[i]! < limit) {
      id += chars[arr[i]! % chars.length];
    } else {
      // Re-roll this byte
      const extra = new Uint8Array(1);
      crypto.getRandomValues(extra);
      arr[i] = extra[0]!;
      continue;
    }
    i++;
  }
  return id;
}

export function useWebSocket({
  sessionId,
  token,
  onData,
  onResize,
  onEnd,
}: UseWebSocketOptions) {
  const wsRef = useRef<WebSocket | null>(null);
  const [connected, setConnected] = useState(false);
  const [joined, setJoined] = useState<JoinedPayload | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [processExited, setProcessExited] = useState<number | null>(null);
  const [fileTransfers, setFileTransfers] = useState<Map<string, FileTransfer>>(new Map());

  // Pending ack resolvers: sendFile waits for the FileStart ack before sending chunks.
  const pendingAcksRef = useRef<Map<string, (ack: FileAckPayload) => void>>(new Map());

  useEffect(() => {
    const proto = window.location.protocol === "https:" ? "wss:" : "ws:";
    const url = `${proto}//${window.location.host}/ws/view/${sessionId}`;

    const ws = new WebSocket(url, "phosphor");
    ws.binaryType = "arraybuffer";
    wsRef.current = ws;

    ws.onopen = () => {
      // Send Join message
      const joinMsg = encode(MsgType.Join, {
        token: token ?? "",
        session_id: sessionId,
      });
      ws.send(joinMsg);
    };

    ws.onmessage = (event) => {
      const [type, payload] = decode(event.data as ArrayBuffer);

      switch (type) {
        case MsgType.Joined: {
          const info = decodeJSON<JoinedPayload>(payload);
          setJoined(info);
          setConnected(true);
          break;
        }
        case MsgType.Stdout:
          onData(payload);
          break;
        case MsgType.Resize: {
          const sz = decodeJSON<{ cols: number; rows: number }>(payload);
          onResize(sz.cols, sz.rows);
          break;
        }
        case MsgType.Reconnect: {
          const info = decodeJSON<ReconnectPayload>(payload);
          if (info.status === "disconnected") {
            setConnected(false);
          } else if (info.status === "reconnected") {
            setConnected(true);
          }
          break;
        }
        case MsgType.ProcessExited: {
          const info = decodeJSON<ProcessExitedPayload>(payload);
          setProcessExited(info.exit_code);
          break;
        }
        case MsgType.Restart:
          setProcessExited(null);
          break;
        case MsgType.End:
          setConnected(false);
          onEnd();
          break;
        case MsgType.Error: {
          const err = decodeJSON<ErrorPayload>(payload);
          setError(`${err.code}: ${err.message}`);
          break;
        }
        case MsgType.FileAck: {
          const ack = decodeJSON<FileAckPayload>(payload);

          // Resolve pending ack promise (for sendFile flow control)
          const resolver = pendingAcksRef.current.get(ack.id);
          if (resolver) {
            pendingAcksRef.current.delete(ack.id);
            resolver(ack);
          }

          setFileTransfers((prev) => {
            const next = new Map(prev);
            const existing = next.get(ack.id);
            if (!existing) return prev;
            switch (ack.status) {
              case "accepted":
                next.set(ack.id, { ...existing, status: "uploading" });
                break;
              case "progress":
                next.set(ack.id, {
                  ...existing,
                  bytesWritten: ack.bytes_written ?? existing.bytesWritten,
                });
                break;
              case "complete":
                next.set(ack.id, {
                  ...existing,
                  status: "complete",
                  bytesWritten: ack.bytes_written ?? existing.size,
                });
                break;
              case "error":
                next.set(ack.id, {
                  ...existing,
                  status: "error",
                  error: ack.error,
                });
                break;
            }
            return next;
          });
          break;
        }
        case MsgType.Ping:
          ws.send(encode(MsgType.Pong));
          break;
      }
    };

    ws.onclose = () => {
      setConnected(false);
      // Reject all pending ack promises so sendFile doesn't hang
      for (const [id, resolver] of pendingAcksRef.current) {
        resolver({ id, status: "error", error: "connection closed" });
      }
      pendingAcksRef.current.clear();
    };

    ws.onerror = () => {
      setError("WebSocket connection failed");
    };

    return () => {
      ws.close();
      wsRef.current = null;
    };
  }, [sessionId, token, onData, onResize, onEnd]);

  const sendStdin = useCallback((data: Uint8Array) => {
    const ws = wsRef.current;
    if (ws?.readyState === WebSocket.OPEN) {
      ws.send(encode(MsgType.Stdin, data));
    }
  }, []);

  const sendResize = useCallback((cols: number, rows: number) => {
    const ws = wsRef.current;
    if (ws?.readyState === WebSocket.OPEN) {
      ws.send(encode(MsgType.Resize, { cols, rows }));
    }
  }, []);

  const sendRestart = useCallback(() => {
    const ws = wsRef.current;
    if (ws?.readyState === WebSocket.OPEN) {
      ws.send(encode(MsgType.Restart));
    }
  }, []);

  // Schedule removal of a completed/errored transfer after a delay
  const scheduleTransferCleanup = useCallback((id: string) => {
    setTimeout(() => {
      setFileTransfers((prev) => {
        const entry = prev.get(id);
        if (!entry || entry.status === "uploading") return prev;
        const next = new Map(prev);
        next.delete(id);
        return next;
      });
    }, COMPLETED_TRANSFER_TTL_MS);
  }, []);

  const sendFile = useCallback(async (file: File) => {
    const ws = wsRef.current;
    if (!ws || ws.readyState !== WebSocket.OPEN) return;

    const id = generateTransferID();

    // Add transfer to state
    setFileTransfers((prev) => {
      const next = new Map(prev);
      next.set(id, {
        id,
        name: file.name,
        size: file.size,
        bytesWritten: 0,
        status: "uploading",
      });
      return next;
    });

    const markError = (errorMsg: string) => {
      setFileTransfers((prev) => {
        const next = new Map(prev);
        const existing = next.get(id);
        if (!existing) return prev;
        next.set(id, { ...existing, status: "error", error: errorMsg });
        return next;
      });
      scheduleTransferCleanup(id);
    };

    try {
      // Send FileStart and wait for ack before sending chunks.
      ws.send(encode(MsgType.FileStart, { id, name: file.name, size: file.size }));

      const startAck = await new Promise<FileAckPayload>((resolve) => {
        pendingAcksRef.current.set(id, resolve);

        // Timeout: reject if no ack within deadline
        setTimeout(() => {
          if (pendingAcksRef.current.has(id)) {
            pendingAcksRef.current.delete(id);
            resolve({ id, status: "error", error: "ack timeout" });
          }
        }, ACK_TIMEOUT_MS);
      });

      if (startAck.status === "error") {
        // FileStart was rejected — error is already shown via setFileTransfers (from FileAck handler)
        // but if the error came from timeout or connection close, mark it explicitly
        markError(startAck.error ?? "upload rejected");
        return;
      }

      // Stream file in chunks using file.slice() to avoid loading entire file into memory
      const textEncoder = new TextEncoder();
      const idBytes = textEncoder.encode(id); // 8 ASCII bytes

      let offset = 0;
      while (offset < file.size) {
        if (ws.readyState !== WebSocket.OPEN) {
          markError("connection lost during upload");
          return;
        }
        const end = Math.min(offset + FILE_CHUNK_SIZE, file.size);
        const blob = file.slice(offset, end);
        const chunkBuffer = await blob.arrayBuffer();
        const chunk = new Uint8Array(chunkBuffer);

        // Build FileChunk payload: [8-byte ID][raw data]
        const chunkPayload = new Uint8Array(TRANSFER_ID_LEN + chunk.length);
        chunkPayload.set(idBytes, 0);
        chunkPayload.set(chunk, TRANSFER_ID_LEN);

        ws.send(encode(MsgType.FileChunk, chunkPayload));
        offset = end;
      }

      // Compute SHA256 by re-reading the file from disk.
      // Web Crypto's digest() has no incremental API, so we read the file
      // a second time rather than accumulating all chunks in memory.
      const fileBuffer = await file.arrayBuffer();
      const hashBuffer = await crypto.subtle.digest("SHA-256", fileBuffer);
      const hashArray = new Uint8Array(hashBuffer);
      const sha256 = Array.from(hashArray)
        .map((b) => b.toString(16).padStart(2, "0"))
        .join("");

      // Send FileEnd
      ws.send(encode(MsgType.FileEnd, { id, sha256 }));

      // Wait for completion ack with timeout
      const completeAck = await new Promise<FileAckPayload>((resolve) => {
        pendingAcksRef.current.set(id, resolve);
        setTimeout(() => {
          if (pendingAcksRef.current.has(id)) {
            pendingAcksRef.current.delete(id);
            resolve({ id, status: "error", error: "completion ack timeout" });
          }
        }, ACK_TIMEOUT_MS);
      });

      if (completeAck.status === "error") {
        markError(completeAck.error ?? "upload failed");
        return;
      }

      // Schedule cleanup after successful completion
      scheduleTransferCleanup(id);
    } catch (err) {
      const errorMsg = err instanceof Error ? err.message : "upload failed";
      markError(errorMsg);
      // Clean up any pending ack resolver
      pendingAcksRef.current.delete(id);
    }
  }, [scheduleTransferCleanup]);

  return {
    connected,
    joined,
    error,
    processExited,
    fileTransfers,
    sendStdin,
    sendResize,
    sendRestart,
    sendFile,
  };
}
