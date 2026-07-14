import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ImportKeyModal } from "./ImportKeyModal";

vi.mock("../lib/wasm", () => ({
  loadSSH: vi.fn(),
}));
vi.mock("../lib/keys", () => ({
  saveKey: vi.fn(),
}));

import { loadSSH } from "../lib/wasm";
import { saveKey } from "../lib/keys";

const mockLoadSSH = vi.mocked(loadSSH);
const mockSaveKey = vi.mocked(saveKey);

const publicKeyFromPem = vi.fn();

beforeEach(() => {
  vi.clearAllMocks();
  mockLoadSSH.mockResolvedValue({
    connect: vi.fn(),
    generateKeypair: vi.fn(),
    publicKeyFromPem,
  });
  mockSaveKey.mockResolvedValue();
});

function renderModal(props: Partial<Parameters<typeof ImportKeyModal>[0]> = {}) {
  const onClose = vi.fn();
  const onImported = vi.fn();
  render(<ImportKeyModal onClose={onClose} onImported={onImported} {...props} />);
  return {
    onClose,
    onImported,
    nameInput: screen.getByLabelText(/key name/i),
    pemInput: screen.getByLabelText(/private key/i),
    passphraseInput: screen.getByLabelText(/passphrase/i),
    importButton: screen.getByRole("button", { name: /\[import\]/i }),
    cancelButton: screen.getByRole("button", { name: /\[cancel\]/i }),
  };
}

const PEM = "-----BEGIN OPENSSH PRIVATE KEY-----\nAAAA\n-----END OPENSSH PRIVATE KEY-----";

describe("ImportKeyModal", () => {
  it("disables import until both name and PEM are non-whitespace", async () => {
    const user = userEvent.setup();
    const { nameInput, pemInput, importButton } = renderModal();

    expect(importButton).toBeDisabled();

    await user.type(nameInput, "laptop");
    expect(importButton).toBeDisabled();

    await user.type(pemInput, "   ");
    expect(importButton).toBeDisabled();

    await user.clear(pemInput);
    await user.type(pemInput, PEM.replaceAll("\n", " "));
    expect(importButton).toBeEnabled();

    await user.clear(nameInput);
    await user.type(nameInput, "   ");
    expect(importButton).toBeDisabled();
  });

  it("saves the derived key and fires onImported on success", async () => {
    publicKeyFromPem.mockReturnValue({
      authorizedKey: "ssh-ed25519 AAAA derived",
      fingerprint: "SHA256:derived",
    });
    const user = userEvent.setup();
    const { nameInput, pemInput, importButton, onImported } = renderModal();

    await user.type(nameInput, "  laptop  ");
    await user.type(pemInput, PEM.replaceAll("\n", " "));
    await user.click(importButton);

    await waitFor(() => expect(onImported).toHaveBeenCalled());
    expect(mockSaveKey).toHaveBeenCalledWith(
      expect.objectContaining({
        name: "laptop",
        authorizedKey: "ssh-ed25519 AAAA derived",
        fingerprint: "SHA256:derived",
        encrypted: false,
      }),
    );
  });

  it("marks the key encrypted when a passphrase was used", async () => {
    publicKeyFromPem.mockReturnValue({
      authorizedKey: "ssh-ed25519 AAAA derived",
      fingerprint: "SHA256:derived",
    });
    const user = userEvent.setup();
    const { nameInput, pemInput, passphraseInput, importButton, onImported } = renderModal();

    await user.type(nameInput, "laptop");
    await user.type(pemInput, "encrypted-pem");
    await user.type(passphraseInput, "secret123");
    await user.click(importButton);

    await waitFor(() => expect(onImported).toHaveBeenCalled());
    expect(publicKeyFromPem).toHaveBeenCalledWith("encrypted-pem", "secret123");
    expect(mockSaveKey).toHaveBeenCalledWith(expect.objectContaining({ encrypted: true }));
  });

  it("shows the error and saves nothing when key parsing fails", async () => {
    publicKeyFromPem.mockImplementation(() => {
      throw new Error("ssh: no key found");
    });
    const user = userEvent.setup();
    const { nameInput, pemInput, importButton, onImported } = renderModal();

    await user.type(nameInput, "laptop");
    await user.type(pemInput, "garbage");
    await user.click(importButton);

    await waitFor(() => expect(screen.getByText("ssh: no key found")).toBeInTheDocument());
    expect(mockSaveKey).not.toHaveBeenCalled();
    expect(onImported).not.toHaveBeenCalled();
    // Modal stays open for correction.
    expect(screen.getByText("// IMPORT KEY")).toBeInTheDocument();
  });

  it("shows the error and saves nothing when persistence fails", async () => {
    publicKeyFromPem.mockReturnValue({
      authorizedKey: "ssh-ed25519 AAAA",
      fingerprint: "SHA256:x",
    });
    mockSaveKey.mockRejectedValue(new Error("quota exceeded"));
    const user = userEvent.setup();
    const { nameInput, pemInput, importButton, onImported } = renderModal();

    await user.type(nameInput, "laptop");
    await user.type(pemInput, "pem");
    await user.click(importButton);

    await waitFor(() => expect(screen.getByText("quota exceeded")).toBeInTheDocument());
    expect(onImported).not.toHaveBeenCalled();
  });

  it("closes via the cancel button and the overlay, but not panel clicks", async () => {
    const user = userEvent.setup();
    const { cancelButton, onClose, nameInput } = renderModal();

    await user.click(nameInput);
    expect(onClose).not.toHaveBeenCalled();

    await user.click(cancelButton);
    expect(onClose).toHaveBeenCalledTimes(1);

    await user.click(document.querySelector(".modal-overlay")!);
    expect(onClose).toHaveBeenCalledTimes(2);
  });
});
