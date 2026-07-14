import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { KeysPage } from "./KeysPage";
import type { StoredKey } from "../lib/keys";

vi.mock("../lib/wasm", () => ({
  loadSSH: vi.fn(),
}));
vi.mock("../lib/keys", () => ({
  listKeys: vi.fn(),
  saveKey: vi.fn(),
  deleteKey: vi.fn(),
}));

import { loadSSH } from "../lib/wasm";
import { listKeys, saveKey, deleteKey } from "../lib/keys";

const mockLoadSSH = vi.mocked(loadSSH);
const mockListKeys = vi.mocked(listKeys);
const mockSaveKey = vi.mocked(saveKey);
const mockDeleteKey = vi.mocked(deleteKey);

const generateKeypair = vi.fn();
const publicKeyFromPem = vi.fn();

function storedKey(overrides: Partial<StoredKey> = {}): StoredKey {
  return {
    id: "key-1",
    name: "laptop",
    privateKeyPem: "PEM",
    authorizedKey: "ssh-ed25519 AAAA laptop",
    fingerprint: "SHA256:laptopfingerprint",
    createdAt: 1700000000000,
    encrypted: false,
    ...overrides,
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  mockLoadSSH.mockResolvedValue({
    connect: vi.fn(),
    generateKeypair,
    publicKeyFromPem,
  });
  mockListKeys.mockResolvedValue([]);
  mockSaveKey.mockResolvedValue();
  mockDeleteKey.mockResolvedValue();
});

describe("KeysPage", () => {
  it("lists stored keys with fingerprints", async () => {
    mockListKeys.mockResolvedValue([storedKey()]);
    render(<KeysPage />);

    expect(await screen.findByText("laptop")).toBeInTheDocument();
    expect(screen.getByText("SHA256:laptopfingerprint")).toBeInTheDocument();
  });

  it("disables generate until a name is entered", async () => {
    const user = userEvent.setup();
    render(<KeysPage />);

    const generate = screen.getByRole("button", { name: /generate ed25519/i });
    expect(generate).toBeDisabled();

    await user.type(screen.getByPlaceholderText(/key name/i), "laptop");
    expect(generate).toBeEnabled();
  });

  it("generates, saves, and refreshes the list", async () => {
    generateKeypair.mockReturnValue({
      privateKeyPem: "GENERATED-PEM",
      authorizedKey: "ssh-ed25519 AAAA generated",
      fingerprint: "SHA256:generated",
    });
    mockListKeys
      .mockResolvedValueOnce([]) // initial load
      .mockResolvedValue([storedKey({ name: "fresh", fingerprint: "SHA256:generated" })]);

    const user = userEvent.setup();
    render(<KeysPage />);

    await user.type(screen.getByPlaceholderText(/key name/i), "fresh");
    await user.click(screen.getByRole("button", { name: /generate ed25519/i }));

    await waitFor(() =>
      expect(mockSaveKey).toHaveBeenCalledWith(
        expect.objectContaining({
          name: "fresh",
          privateKeyPem: "GENERATED-PEM",
          fingerprint: "SHA256:generated",
          encrypted: false,
        }),
      ),
    );
    expect(await screen.findByText("SHA256:generated")).toBeInTheDocument();
  });

  it("shows the error and saves nothing when generation fails", async () => {
    generateKeypair.mockImplementation(() => {
      throw new Error("entropy failure");
    });
    const user = userEvent.setup();
    render(<KeysPage />);

    await user.type(screen.getByPlaceholderText(/key name/i), "doomed");
    await user.click(screen.getByRole("button", { name: /generate ed25519/i }));

    expect(await screen.findByText("entropy failure")).toBeInTheDocument();
    expect(mockSaveKey).not.toHaveBeenCalled();
  });

  it("opens the import modal, and a completed import closes it and refreshes", async () => {
    publicKeyFromPem.mockReturnValue({
      authorizedKey: "ssh-ed25519 AAAA imported",
      fingerprint: "SHA256:imported",
    });
    mockListKeys
      .mockResolvedValueOnce([]) // initial load
      .mockResolvedValue([storedKey({ name: "imported-key", fingerprint: "SHA256:imported" })]);

    const user = userEvent.setup();
    render(<KeysPage />);

    await user.click(screen.getByRole("button", { name: /import existing key/i }));
    expect(screen.getByText("// IMPORT KEY")).toBeInTheDocument();

    await user.type(screen.getByLabelText(/key name/i), "imported-key");
    await user.type(screen.getByLabelText(/private key/i), "pasted-pem");
    await user.click(screen.getByRole("button", { name: /\[import\]/i }));

    await waitFor(() => expect(screen.queryByText("// IMPORT KEY")).not.toBeInTheDocument());
    expect(mockSaveKey).toHaveBeenCalledWith(expect.objectContaining({ name: "imported-key" }));
    expect(await screen.findByText("SHA256:imported")).toBeInTheDocument();
  });

  it("deletes a key and refreshes", async () => {
    mockListKeys.mockResolvedValueOnce([storedKey()]).mockResolvedValue([]);
    const user = userEvent.setup();
    render(<KeysPage />);

    await screen.findByText("laptop");
    await user.click(screen.getByRole("button", { name: /delete/i }));

    await waitFor(() => expect(mockDeleteKey).toHaveBeenCalledWith("key-1"));
    expect(await screen.findByText("No keys yet.")).toBeInTheDocument();
  });
});
