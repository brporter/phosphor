import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Routes, Route } from "react-router";
import { ConnectView } from "./ConnectView";
import type { UseSSHOptions, UseSSHResult } from "../hooks/useSSH";
import type { StoredKey } from "../lib/keys";

const sshSpy = vi.hoisted(() => ({
  calls: [] as UseSSHOptions[],
}));

vi.mock("../hooks/useSSH", () => ({
  useSSH: (opts: UseSSHOptions): UseSSHResult => {
    sshSpy.calls.push(opts);
    return {
      connected: false,
      connecting: false,
      error: null,
      authRequest: null,
      sendStdin: vi.fn(),
      sendResize: vi.fn(),
      disconnect: vi.fn(),
    };
  },
}));
vi.mock("../auth/useAuth", () => ({
  useAuth: () => ({ getToken: () => "test-token" }),
}));
vi.mock("../lib/keys", () => ({
  listKeys: vi.fn(),
  saveKey: vi.fn(),
}));
vi.mock("../lib/wasm", () => ({
  loadSSH: vi.fn(),
}));
// xterm needs a real DOM/canvas; the terminal itself is out of scope here.
vi.mock("@xterm/xterm", () => ({
  Terminal: class {
    cols = 80;
    rows = 24;
    open() {}
    loadAddon() {}
    attachCustomKeyEventHandler() {}
    onData() {
      return { dispose() {} };
    }
    onResize() {
      return { dispose() {} };
    }
    write() {}
    dispose() {}
  },
}));
vi.mock("@xterm/addon-fit", () => ({ FitAddon: class { fit() {} } }));
vi.mock("@xterm/addon-webgl", () => ({ WebglAddon: class {} }));

import { listKeys, saveKey } from "../lib/keys";
import { loadSSH } from "../lib/wasm";

const mockListKeys = vi.mocked(listKeys);
const mockSaveKey = vi.mocked(saveKey);
const mockLoadSSH = vi.mocked(loadSSH);
const publicKeyFromPem = vi.fn();

const PEM = "-----BEGIN OPENSSH PRIVATE KEY----- AAAA -----END OPENSSH PRIVATE KEY-----";
const INLINE_OPTION = /use a key just for this session/i;

function storedKey(): StoredKey {
  return {
    id: "key-1",
    name: "laptop",
    privateKeyPem: "STORED-PEM",
    authorizedKey: "ssh-ed25519 AAAA laptop",
    fingerprint: "SHA256:laptopfingerprint1234567890",
    createdAt: 1700000000000,
    encrypted: false,
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  sshSpy.calls.length = 0;
  localStorage.clear();
  mockListKeys.mockResolvedValue([]);
  mockLoadSSH.mockResolvedValue({
    connect: vi.fn(),
    generateKeypair: vi.fn(),
    publicKeyFromPem,
  });
  publicKeyFromPem.mockReturnValue({
    authorizedKey: "ssh-ed25519 AAAA valid",
    fingerprint: "SHA256:valid",
  });
});

function renderConnect() {
  render(
    <MemoryRouter initialEntries={["/machine/m-1"]}>
      <Routes>
        <Route path="/machine/:id" element={<ConnectView />} />
      </Routes>
    </MemoryRouter>,
  );
}

function lastSSHCall(): UseSSHOptions {
  return sshSpy.calls[sshSpy.calls.length - 1];
}

describe("ConnectView key selection", () => {
  it("reveals the inline key fields only when the session-only option is chosen", async () => {
    const user = userEvent.setup();
    renderConnect();

    expect(screen.queryByLabelText(/private key/i)).not.toBeInTheDocument();

    await user.selectOptions(screen.getByLabelText(/key \(optional/i), screen.getByRole("option", { name: INLINE_OPTION }));

    expect(screen.getByLabelText(/private key/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/key passphrase/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /load from file/i })).toBeInTheDocument();
  });

  it("disables connect while the inline key is selected but empty", async () => {
    const user = userEvent.setup();
    renderConnect();

    await user.type(screen.getByLabelText(/ssh username/i), "ubuntu");
    const connect = screen.getByRole("button", { name: /\[connect\]/i });
    expect(connect).toBeEnabled();

    await user.selectOptions(screen.getByLabelText(/key \(optional/i), screen.getByRole("option", { name: INLINE_OPTION }));
    expect(connect).toBeDisabled();

    await user.type(screen.getByLabelText(/private key/i), PEM);
    expect(connect).toBeEnabled();
  });

  it("rejects an invalid inline key with an error and does not start the session", async () => {
    publicKeyFromPem.mockImplementation(() => {
      throw new Error("ssh: no key found");
    });
    const user = userEvent.setup();
    renderConnect();

    await user.type(screen.getByLabelText(/ssh username/i), "ubuntu");
    await user.selectOptions(screen.getByLabelText(/key \(optional/i), screen.getByRole("option", { name: INLINE_OPTION }));
    await user.type(screen.getByLabelText(/private key/i), "garbage");
    await user.click(screen.getByRole("button", { name: /\[connect\]/i }));

    expect(await screen.findByText("ssh: no key found")).toBeInTheDocument();
    // Still on the form, and the SSH hook was never told to connect.
    expect(screen.getByLabelText(/ssh username/i)).toBeInTheDocument();
    expect(sshSpy.calls.every((c) => !c.connect)).toBe(true);
  });

  it("starts the session with the inline key and passphrase, never persisting it", async () => {
    const user = userEvent.setup();
    renderConnect();

    await user.type(screen.getByLabelText(/ssh username/i), "ubuntu");
    await user.selectOptions(screen.getByLabelText(/key \(optional/i), screen.getByRole("option", { name: INLINE_OPTION }));
    await user.type(screen.getByLabelText(/private key/i), PEM);
    await user.type(screen.getByLabelText(/key passphrase/i), "secret123");
    await user.click(screen.getByRole("button", { name: /\[connect\]/i }));

    await waitFor(() => expect(lastSSHCall().connect).toBe(true));
    expect(publicKeyFromPem).toHaveBeenCalledWith(PEM, "secret123");
    expect(lastSSHCall().key).toEqual({ privateKeyPem: PEM, passphrase: "secret123" });
    expect(mockSaveKey).not.toHaveBeenCalled();
  });

  it("maps a stored key selection to a session key without validation round-trip", async () => {
    mockListKeys.mockResolvedValue([storedKey()]);
    const user = userEvent.setup();
    renderConnect();

    await screen.findByRole("option", { name: /laptop/i });
    await user.type(screen.getByLabelText(/ssh username/i), "ubuntu");
    await user.selectOptions(screen.getByLabelText(/key \(optional/i), "key-1");
    await user.click(screen.getByRole("button", { name: /\[connect\]/i }));

    await waitFor(() => expect(lastSSHCall().connect).toBe(true));
    expect(lastSSHCall().key).toEqual({ privateKeyPem: "STORED-PEM" });
    expect(publicKeyFromPem).not.toHaveBeenCalled();
  });

  it("keeps the session key referentially stable across unrelated re-renders", async () => {
    const user = userEvent.setup();
    renderConnect();

    await user.selectOptions(screen.getByLabelText(/key \(optional/i), screen.getByRole("option", { name: INLINE_OPTION }));
    await user.type(screen.getByLabelText(/private key/i), PEM);

    const keyAfterPaste = lastSSHCall().key;
    // Unrelated state change re-renders the component.
    await user.type(screen.getByLabelText(/ssh username/i), "ubuntu");

    expect(lastSSHCall().key).toBe(keyAfterPaste);
  });

  it("loads a key file into the PEM textarea", async () => {
    const user = userEvent.setup();
    renderConnect();

    await user.selectOptions(screen.getByLabelText(/key \(optional/i), screen.getByRole("option", { name: INLINE_OPTION }));

    const fileInput = document.querySelector<HTMLInputElement>('input[type="file"]')!;
    const file = new File([PEM], "id_ed25519", { type: "text/plain" });
    fireEvent.change(fileInput, { target: { files: [file] } });

    await waitFor(() => expect(screen.getByLabelText(/private key/i)).toHaveValue(PEM));
  });
});
