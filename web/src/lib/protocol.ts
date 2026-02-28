// Binary protocol: [type_byte][payload]
// Mirrors internal/protocol/messages.go

export const MsgType = {
  Stdout: 0x01,
  Stdin: 0x02,
  Resize: 0x03,
  Hello: 0x10,
  Welcome: 0x11,
  Join: 0x12,
  Joined: 0x13,
  End: 0x15,
  Error: 0x16,
  ViewerCount: 0x20,
  Mode: 0x21,
  Ping: 0x30,
  Pong: 0x31,
} as const;

export type MsgTypeValue = (typeof MsgType)[keyof typeof MsgType];

export interface JoinedPayload {
  mode: string;
  cols: number;
  rows: number;
  command: string;
}

export interface ErrorPayload {
  code: string;
  message: string;
}

export interface ResizePayload {
  cols: number;
  rows: number;
}

const encoder = new TextEncoder();
const decoder = new TextDecoder();

/**
 * Encode a protocol message.
 * For Stdin: payload is Uint8Array (raw key bytes).
 * For control messages: payload is JSON-serializable.
 */
export function encode(type: number, payload?: unknown): ArrayBuffer {
  if (type === MsgType.Stdin) {
    const data = payload as Uint8Array;
    const buf = new Uint8Array(1 + data.length);
    buf[0] = type;
    buf.set(data, 1);
    return buf.buffer;
  }

  if (
    type === MsgType.Ping ||
    type === MsgType.Pong ||
    payload === undefined
  ) {
    return new Uint8Array([type]).buffer;
  }

  const json = encoder.encode(JSON.stringify(payload));
  const buf = new Uint8Array(1 + json.length);
  buf[0] = type;
  buf.set(json, 1);
  return buf.buffer;
}

/**
 * Decode a binary message into [type, rawPayload].
 */
export function decode(data: ArrayBuffer): [number, Uint8Array] {
  const bytes = new Uint8Array(data);
  return [bytes[0]!, bytes.slice(1)];
}

/**
 * Decode JSON payload.
 */
export function decodeJSON<T>(payload: Uint8Array): T {
  return JSON.parse(decoder.decode(payload)) as T;
}
