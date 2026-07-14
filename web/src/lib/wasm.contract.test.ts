// @vitest-environment node
//
// Contract tests for the WASM ↔ JS boundary: loads the real phosphor-ssh.wasm
// artifact and asserts the {ok, ...} envelope shape of the synchronous API.
// Neither `go test` nor `tsc` can see this boundary — a drift here (e.g. the
// old bug of returning an Error instance instead of an envelope) breaks the
// app while both sides' checks stay green.
//
// Requires `make wasm` to have produced web/public/phosphor-ssh.wasm; the
// suite skips (visibly) when the artifact is missing.

import { readFileSync, existsSync } from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";

const publicDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../public");
const wasmPath = path.join(publicDir, "phosphor-ssh.wasm");
const execPath = path.join(publicDir, "wasm_exec.js");
const artifactsExist = existsSync(wasmPath) && existsSync(execPath);

// The raw global, before the wasm.ts wrapper: envelopes, not exceptions.
interface RawEnvelope {
  ok: boolean;
  error?: string;
  privateKeyPem?: string;
  authorizedKey?: string;
  fingerprint?: string;
}
interface RawPhosphorSSH {
  generateKeypair(passphrase?: string): RawEnvelope;
  publicKeyFromPem(pem: string, passphrase?: string): RawEnvelope;
}

function expectSuccess(result: RawEnvelope) {
  expect(result).not.toBeInstanceOf(Error);
  expect(result.ok).toBe(true);
  expect(result).not.toHaveProperty("error");
  expect(result.authorizedKey).toMatch(/^ssh-/);
  expect(result.fingerprint).toMatch(/^SHA256:/);
}

function expectFailure(result: RawEnvelope) {
  expect(result).not.toBeInstanceOf(Error);
  expect(result.ok).toBe(false);
  expect(typeof result.error).toBe("string");
  expect(result.error).not.toBe("");
  expect(result).not.toHaveProperty("authorizedKey");
  expect(result).not.toHaveProperty("fingerprint");
  expect(result).not.toHaveProperty("privateKeyPem");
}

describe.skipIf(!artifactsExist)("phosphor-ssh.wasm envelope contract", () => {
  let ssh: RawPhosphorSSH;

  beforeAll(async () => {
    // wasm_exec.js installs the Go class on globalThis.
    // eslint-disable-next-line no-eval
    (0, eval)(readFileSync(execPath, "utf8"));
    const Go = (globalThis as Record<string, any>).Go;
    const go = new Go();
    const { instance } = await WebAssembly.instantiate(readFileSync(wasmPath), go.importObject);
    void go.run(instance); // never resolves: main() blocks in select{}
    for (let i = 0; i < 100 && !(globalThis as Record<string, any>).phosphorSSH; i++) {
      await new Promise((r) => setTimeout(r, 10));
    }
    ssh = (globalThis as Record<string, any>).phosphorSSH;
    expect(ssh).toBeDefined();
  }, 30_000);

  it("generateKeypair returns an ok envelope with all key fields", () => {
    const result = ssh.generateKeypair();
    expectSuccess(result);
    expect(result.privateKeyPem).toContain("OPENSSH PRIVATE KEY");
  });

  it("publicKeyFromPem round-trips a generated key", () => {
    const kp = ssh.generateKeypair();
    const info = ssh.publicKeyFromPem(kp.privateKeyPem!);
    expectSuccess(info);
    expect(info.fingerprint).toBe(kp.fingerprint);
    expect(info.authorizedKey).toBe(kp.authorizedKey);
  });

  it("returns a failure envelope for garbage input", () => {
    const result = ssh.publicKeyFromPem("not a key");
    expectFailure(result);
    expect(result.error).toContain("no key found");
  });

  it("returns a failure envelope when arguments are missing", () => {
    const result = (ssh.publicKeyFromPem as () => RawEnvelope)();
    expectFailure(result);
  });

  it("encrypted key: missing and wrong passphrases fail, correct one succeeds", { timeout: 30_000 }, () => {
    const kp = ssh.generateKeypair("secret123");
    expectSuccess(kp);

    const missing = ssh.publicKeyFromPem(kp.privateKeyPem!);
    expectFailure(missing);
    expect(missing.error).toContain("passphrase protected");

    const wrong = ssh.publicKeyFromPem(kp.privateKeyPem!, "nope");
    expectFailure(wrong);

    const correct = ssh.publicKeyFromPem(kp.privateKeyPem!, "secret123");
    expectSuccess(correct);
    expect(correct.fingerprint).toBe(kp.fingerprint);
  });
});

// Make the skip loud rather than silent.
if (!artifactsExist) {
  it("wasm contract suite skipped — run `make wasm` to build web/public/phosphor-ssh.wasm", () => {
    console.warn(`missing artifact: ${wasmPath}`);
    expect(artifactsExist).toBe(false);
  });
}
