import { encode, decode, decodeJSON, MsgType, JoinedPayload } from './protocol';

describe('encode', () => {
  it('encodes Stdin with raw Uint8Array payload', () => {
    const data = new Uint8Array([0x68, 0x69]); // "hi"
    const buf = encode(MsgType.Stdin, data);
    const bytes = new Uint8Array(buf);
    expect(bytes.length).toBe(3);
    expect(bytes[0]).toBe(0x02);
    expect(bytes[1]).toBe(0x68);
    expect(bytes[2]).toBe(0x69);
  });

  it('encodes Ping as a single byte', () => {
    const buf = encode(MsgType.Ping);
    const bytes = new Uint8Array(buf);
    expect(bytes.length).toBe(1);
    expect(bytes[0]).toBe(0x30);
  });

  it('encodes Pong as a single byte', () => {
    const buf = encode(MsgType.Pong);
    const bytes = new Uint8Array(buf);
    expect(bytes.length).toBe(1);
    expect(bytes[0]).toBe(0x31);
  });

  it('encodes a JSON type (Join) with type prefix followed by JSON bytes', () => {
    const payload = { token: 'abc123' };
    const buf = encode(MsgType.Join, payload);
    const bytes = new Uint8Array(buf);
    expect(bytes[0]).toBe(MsgType.Join);
    const jsonStr = new TextDecoder().decode(bytes.slice(1));
    expect(JSON.parse(jsonStr)).toEqual(payload);
  });

  it('encodes with undefined payload as a single byte', () => {
    const buf = encode(MsgType.End, undefined);
    const bytes = new Uint8Array(buf);
    expect(bytes.length).toBe(1);
    expect(bytes[0]).toBe(MsgType.End);
  });

  it('encodes with no payload argument as a single byte', () => {
    const buf = encode(MsgType.Hello);
    const bytes = new Uint8Array(buf);
    expect(bytes.length).toBe(1);
    expect(bytes[0]).toBe(MsgType.Hello);
  });
});

describe('decode', () => {
  it('splits a multi-byte message into type and payload', () => {
    const json = new TextEncoder().encode(JSON.stringify({ cols: 80, rows: 24 }));
    const buf = new Uint8Array(1 + json.length);
    buf[0] = MsgType.Resize;
    buf.set(json, 1);

    const [type, payload] = decode(buf.buffer);
    expect(type).toBe(MsgType.Resize);
    expect(Array.from(payload)).toEqual(Array.from(json));
  });

  it('decodes a single-byte message with empty payload', () => {
    const buf = new Uint8Array([MsgType.Ping]);
    const [type, payload] = decode(buf.buffer);
    expect(type).toBe(MsgType.Ping);
    expect(payload.length).toBe(0);
  });
});

describe('decodeJSON', () => {
  it('parses a JoinedPayload from a Uint8Array', () => {
    const obj: JoinedPayload = { mode: 'pty', cols: 120, rows: 40, command: 'bash' };
    const encoded = new TextEncoder().encode(JSON.stringify(obj));
    const result = decodeJSON<JoinedPayload>(encoded);
    expect(result.mode).toBe('pty');
    expect(result.cols).toBe(120);
    expect(result.rows).toBe(40);
    expect(result.command).toBe('bash');
  });
});

describe('encode/decode round-trip', () => {
  it('encodes a Join payload and decodes it back to the original fields', () => {
    const original: JoinedPayload = { mode: 'pty', cols: 200, rows: 50, command: 'zsh' };
    const buf = encode(MsgType.Joined, original);

    const [type, payload] = decode(buf);
    expect(type).toBe(MsgType.Joined);

    const result = decodeJSON<JoinedPayload>(payload);
    expect(result.mode).toBe(original.mode);
    expect(result.cols).toBe(original.cols);
    expect(result.rows).toBe(original.rows);
    expect(result.command).toBe(original.command);
  });
});
