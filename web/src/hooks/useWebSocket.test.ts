import { renderHook, act } from "@testing-library/react";
import { useWebSocket } from "./useWebSocket";
import { encode, MsgType } from "../lib/protocol";

class MockWebSocket {
  static instances: MockWebSocket[] = [];

  url: string;
  protocol: string;
  binaryType = "blob";
  readyState = WebSocket.CONNECTING;

  onopen: ((ev: Event) => void) | null = null;
  onclose: ((ev: CloseEvent) => void) | null = null;
  onmessage: ((ev: MessageEvent) => void) | null = null;
  onerror: ((ev: Event) => void) | null = null;

  send = vi.fn();
  close = vi.fn();

  constructor(url: string, protocol?: string | string[]) {
    this.url = url;
    this.protocol = typeof protocol === "string" ? protocol : protocol?.[0] ?? "";
    MockWebSocket.instances.push(this);
  }

  simulateOpen() {
    this.readyState = WebSocket.OPEN;
    this.onopen?.(new Event("open"));
  }

  simulateMessage(data: ArrayBuffer) {
    this.onmessage?.(new MessageEvent("message", { data }));
  }

  simulateClose() {
    this.readyState = WebSocket.CLOSED;
    this.onclose?.(new CloseEvent("close"));
  }

  simulateError() {
    this.onerror?.(new Event("error"));
  }
}

beforeEach(() => {
  MockWebSocket.instances = [];
  vi.stubGlobal("WebSocket", MockWebSocket);
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("useWebSocket", () => {
  it("sends Join on connect and URL includes sessionId", () => {
    const onData = vi.fn();
    const onResize = vi.fn();
    const onEnd = vi.fn();

    renderHook(() =>
      useWebSocket({
        sessionId: "test-session",
        token: "test-token",
        onData,
        onResize,
        onEnd,
      })
    );

    const ws = MockWebSocket.instances[0]!;
    expect(ws.url).toContain("test-session");

    act(() => {
      ws.simulateOpen();
    });

    expect(ws.send).toHaveBeenCalledTimes(1);
    const sentData = ws.send.mock.calls[0][0] as ArrayBuffer;
    // The first byte should be the Join message type
    const view = new Uint8Array(sentData);
    expect(view[0]).toBe(MsgType.Join);
  });

  it("sets joined and connected on Joined message", () => {
    const onData = vi.fn();
    const onResize = vi.fn();
    const onEnd = vi.fn();

    const { result } = renderHook(() =>
      useWebSocket({
        sessionId: "test-session",
        token: "test-token",
        onData,
        onResize,
        onEnd,
      })
    );

    const ws = MockWebSocket.instances[0]!;

    act(() => {
      ws.simulateOpen();
    });

    const joinedData = encode(MsgType.Joined, {
      mode: "pty",
      cols: 80,
      rows: 24,
      command: "bash",
    });

    act(() => {
      ws.simulateMessage(joinedData);
    });

    expect(result.current.joined).not.toBeNull();
    expect(result.current.joined?.cols).toBe(80);
    expect(result.current.joined?.rows).toBe(24);
    expect(result.current.connected).toBe(true);
  });

  it("forwards Stdout to onData", () => {
    const onData = vi.fn();
    const onResize = vi.fn();
    const onEnd = vi.fn();

    renderHook(() =>
      useWebSocket({
        sessionId: "test-session",
        token: "test-token",
        onData,
        onResize,
        onEnd,
      })
    );

    const ws = MockWebSocket.instances[0]!;

    act(() => {
      ws.simulateOpen();
    });

    const joinedData = encode(MsgType.Joined, {
      mode: "pty",
      cols: 80,
      rows: 24,
      command: "bash",
    });

    act(() => {
      ws.simulateMessage(joinedData);
    });

    const stdoutPayload = new Uint8Array([0x68, 0x65, 0x6c, 0x6c, 0x6f]); // "hello"
    // Construct raw Stdout message manually: [type_byte | raw_payload]
    // (encode() only handles Stdin as raw bytes; Stdout is sent by the server)
    const stdoutMsg = new Uint8Array(1 + stdoutPayload.length);
    stdoutMsg[0] = MsgType.Stdout;
    stdoutMsg.set(stdoutPayload, 1);

    act(() => {
      ws.simulateMessage(stdoutMsg.buffer);
    });

    expect(onData).toHaveBeenCalledTimes(1);
    const receivedPayload = onData.mock.calls[0][0] as Uint8Array;
    expect(Array.from(receivedPayload)).toEqual(Array.from(stdoutPayload));
  });

  it("responds to Ping with Pong", () => {
    const onData = vi.fn();
    const onResize = vi.fn();
    const onEnd = vi.fn();

    renderHook(() =>
      useWebSocket({
        sessionId: "test-session",
        token: "test-token",
        onData,
        onResize,
        onEnd,
      })
    );

    const ws = MockWebSocket.instances[0]!;

    act(() => {
      ws.simulateOpen();
    });

    // Clear the Join send call
    ws.send.mockClear();

    const pingData = encode(MsgType.Ping);

    act(() => {
      ws.simulateMessage(pingData);
    });

    expect(ws.send).toHaveBeenCalledTimes(1);
    const sentData = ws.send.mock.calls[0][0] as ArrayBuffer;
    const view = new Uint8Array(sentData);
    expect(view[0]).toBe(MsgType.Pong);
  });

  it("sets error on Error message with code and message", () => {
    const onData = vi.fn();
    const onResize = vi.fn();
    const onEnd = vi.fn();

    const { result } = renderHook(() =>
      useWebSocket({
        sessionId: "test-session",
        token: "test-token",
        onData,
        onResize,
        onEnd,
      })
    );

    const ws = MockWebSocket.instances[0]!;

    act(() => {
      ws.simulateOpen();
    });

    const errorData = encode(MsgType.Error, {
      code: "unauthorized",
      message: "invalid token",
    });

    act(() => {
      ws.simulateMessage(errorData);
    });

    expect(result.current.error).not.toBeNull();
    expect(result.current.error).toContain("unauthorized");
    expect(result.current.error).toContain("invalid token");
  });
});
