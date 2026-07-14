// Unit tests for the envelope-unwrapping wrapper in wasm.ts: a {ok: false}
// envelope from the WASM module must surface as a thrown Error (the original
// bug returned Error objects, leaving UI try/catch dead and saving corrupt
// keys).

import { wrapPhosphorSSH, type RawPhosphorSSH, type SSHHandle } from "./wasm";

function makeRaw(overrides: Partial<RawPhosphorSSH> = {}): RawPhosphorSSH {
  return {
    connect: vi.fn(),
    generateKeypair: vi.fn(),
    publicKeyFromPem: vi.fn(),
    ...overrides,
  } as RawPhosphorSSH;
}

describe("wrapPhosphorSSH", () => {
  it("throws an Error carrying the envelope message on {ok: false}", () => {
    const wrapped = wrapPhosphorSSH(
      makeRaw({
        publicKeyFromPem: () => ({ ok: false, error: "ssh: no key found" }),
      }),
    );
    expect(() => wrapped.publicKeyFromPem("garbage")).toThrowError("ssh: no key found");
  });

  it("returns the fields on {ok: true}", () => {
    const wrapped = wrapPhosphorSSH(
      makeRaw({
        publicKeyFromPem: () => ({
          ok: true,
          authorizedKey: "ssh-ed25519 AAAA test",
          fingerprint: "SHA256:abc",
        }),
      }),
    );
    const info = wrapped.publicKeyFromPem("valid pem");
    expect(info.authorizedKey).toBe("ssh-ed25519 AAAA test");
    expect(info.fingerprint).toBe("SHA256:abc");
  });

  it("passes pem and passphrase through to the raw call", () => {
    const publicKeyFromPem = vi.fn().mockReturnValue({
      ok: true,
      authorizedKey: "ssh-ed25519 AAAA",
      fingerprint: "SHA256:x",
    });
    const wrapped = wrapPhosphorSSH(makeRaw({ publicKeyFromPem }));
    wrapped.publicKeyFromPem("PEM", "secret");
    expect(publicKeyFromPem).toHaveBeenCalledWith("PEM", "secret");
  });

  it("unwraps generateKeypair the same way", () => {
    const wrapped = wrapPhosphorSSH(
      makeRaw({
        generateKeypair: () => ({ ok: false, error: "entropy failure" }),
      }),
    );
    expect(() => wrapped.generateKeypair()).toThrowError("entropy failure");
  });

  it("leaves connect promise semantics untouched", async () => {
    const handle = { write: vi.fn(), resize: vi.fn(), disconnect: vi.fn() } as SSHHandle;
    const connect = vi.fn().mockResolvedValue(handle);
    const wrapped = wrapPhosphorSSH(makeRaw({ connect }));
    const opts = { wsURL: "ws://x", token: null, username: "u", callbacks: {} };
    await expect(wrapped.connect(opts)).resolves.toBe(handle);
    expect(connect).toHaveBeenCalledWith(opts);
  });
});
