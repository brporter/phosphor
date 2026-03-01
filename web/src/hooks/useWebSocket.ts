import { useCallback, useEffect, useRef, useState } from "react";
import {
  MsgType,
  decode,
  decodeJSON,
  encode,
  type JoinedPayload,
  type ErrorPayload,
  type ReconnectPayload,
} from "../lib/protocol";

interface UseWebSocketOptions {
  sessionId: string;
  token: string | null;
  onData: (data: Uint8Array) => void;
  onResize: (cols: number, rows: number) => void;
  onEnd: () => void;
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
        case MsgType.End:
          setConnected(false);
          onEnd();
          break;
        case MsgType.Error: {
          const err = decodeJSON<ErrorPayload>(payload);
          setError(`${err.code}: ${err.message}`);
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

  return { connected, joined, error, sendStdin, sendResize };
}
