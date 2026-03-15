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
  let id = "";
  const arr = new Uint8Array(TRANSFER_ID_LEN);
  crypto.getRandomValues(arr);
  for (let i = 0; i < TRANSFER_ID_LEN; i++) {
    id += chars[arr[i]! % chars.length];
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

    // Send FileStart and wait for ack before sending chunks.
    // This ensures we stop early if the CLI rejects the transfer.
    ws.send(encode(MsgType.FileStart, { id, name: file.name, size: file.size }));

    const startAck = await new Promise<FileAckPayload>((resolve) => {
      pendingAcksRef.current.set(id, resolve);
    });

    if (startAck.status === "error") {
      // FileStart was rejected — error is already shown via setFileTransfers
      return;
    }

    // Read entire file as ArrayBuffer
    const fileBuffer = await file.arrayBuffer();
    const fileData = new Uint8Array(fileBuffer);
    const textEncoder = new TextEncoder();
    const idBytes = textEncoder.encode(id); // 8 ASCII bytes

    // Send chunks
    let offset = 0;
    while (offset < fileData.length) {
      if (ws.readyState !== WebSocket.OPEN) return;
      const end = Math.min(offset + FILE_CHUNK_SIZE, fileData.length);
      const chunk = fileData.slice(offset, end);

      // Build FileChunk payload: [8-byte ID][raw data]
      const chunkPayload = new Uint8Array(TRANSFER_ID_LEN + chunk.length);
      chunkPayload.set(idBytes, 0);
      chunkPayload.set(chunk, TRANSFER_ID_LEN);

      ws.send(encode(MsgType.FileChunk, chunkPayload));
      offset = end;
    }

    // Compute SHA256
    const hashBuffer = await crypto.subtle.digest("SHA-256", fileData);
    const hashArray = new Uint8Array(hashBuffer);
    const sha256 = Array.from(hashArray)
      .map((b) => b.toString(16).padStart(2, "0"))
      .join("");

    // Send FileEnd
    ws.send(encode(MsgType.FileEnd, { id, sha256 }));
  }, []);

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
