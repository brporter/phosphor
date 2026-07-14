import { renderHook, waitFor } from "@testing-library/react";
import { useSSH, type UseSSHOptions } from "./useSSH";

vi.mock("../lib/wasm", () => ({
  loadSSH: vi.fn(),
}));

import { loadSSH } from "../lib/wasm";

const mockLoadSSH = vi.mocked(loadSSH);
const connect = vi.fn();
const handle = { write: vi.fn(), resize: vi.fn(), disconnect: vi.fn() };

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
  connect.mockResolvedValue(handle);
  mockLoadSSH.mockResolvedValue({
    connect,
    generateKeypair: vi.fn(),
    publicKeyFromPem: vi.fn(),
  });
});

function baseOptions(overrides: Partial<UseSSHOptions> = {}): UseSSHOptions {
  return {
    machineId: "m-1",
    token: "tok",
    username: "ubuntu",
    key: null,
    connect: true,
    onData: vi.fn(),
    ...overrides,
  };
}

describe("useSSH", () => {
  it("forwards the session key PEM and passphrase to the WASM client", async () => {
    renderHook((props: UseSSHOptions) => useSSH(props), {
      initialProps: baseOptions({ key: { privateKeyPem: "PEM", passphrase: "secret" } }),
    });

    await waitFor(() => expect(connect).toHaveBeenCalled());
    expect(connect).toHaveBeenCalledWith(
      expect.objectContaining({
        username: "ubuntu",
        token: "tok",
        privateKey: "PEM",
        keyPassphrase: "secret",
      }),
    );
  });

  it("omits key material for password authentication", async () => {
    renderHook((props: UseSSHOptions) => useSSH(props), {
      initialProps: baseOptions({ key: null }),
    });

    await waitFor(() => expect(connect).toHaveBeenCalled());
    expect(connect).toHaveBeenCalledWith(
      expect.objectContaining({ privateKey: undefined, keyPassphrase: undefined }),
    );
  });

  it("does not reconnect when the key identity changes after the session starts", async () => {
    const { rerender } = renderHook((props: UseSSHOptions) => useSSH(props), {
      initialProps: baseOptions({ key: { privateKeyPem: "PEM" } }),
    });

    await waitFor(() => expect(connect).toHaveBeenCalledTimes(1));

    rerender(baseOptions({ key: { privateKeyPem: "PEM" } })); // new object, same content
    await new Promise((r) => setTimeout(r, 20));
    expect(connect).toHaveBeenCalledTimes(1);
  });

  it("reports connected once the WASM client resolves", async () => {
    const { result } = renderHook((props: UseSSHOptions) => useSSH(props), {
      initialProps: baseOptions(),
    });

    await waitFor(() => expect(result.current.connected).toBe(true));
    expect(result.current.error).toBeNull();
  });

  it("surfaces connect failures as error state", async () => {
    connect.mockRejectedValue(new Error("ssh handshake: auth failed"));
    const { result } = renderHook((props: UseSSHOptions) => useSSH(props), {
      initialProps: baseOptions(),
    });

    await waitFor(() => expect(result.current.error).toBe("ssh handshake: auth failed"));
    expect(result.current.connected).toBe(false);
  });
});
