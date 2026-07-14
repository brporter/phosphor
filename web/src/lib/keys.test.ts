// IndexedDB round-trip tests for browser key storage, backed by fake-indexeddb.

import "fake-indexeddb/auto";
import { listKeys, saveKey, deleteKey, type StoredKey } from "./keys";

function makeKey(overrides: Partial<StoredKey> = {}): StoredKey {
  return {
    id: crypto.randomUUID(),
    name: "test-key",
    privateKeyPem: "-----BEGIN OPENSSH PRIVATE KEY-----\nAAAA\n-----END OPENSSH PRIVATE KEY-----\n",
    authorizedKey: "ssh-ed25519 AAAA test",
    fingerprint: "SHA256:testfingerprint",
    createdAt: 1700000000000,
    encrypted: false,
    ...overrides,
  };
}

describe("key storage", () => {
  it("saves, lists, and deletes keys", async () => {
    const key = makeKey();
    await saveKey(key);

    let keys = await listKeys();
    expect(keys.map((k) => k.id)).toContain(key.id);
    const stored = keys.find((k) => k.id === key.id)!;
    expect(stored).toEqual(key);

    await deleteKey(key.id);
    keys = await listKeys();
    expect(keys.map((k) => k.id)).not.toContain(key.id);
  });

  it("overwrites a key with the same id", async () => {
    const key = makeKey({ name: "before" });
    await saveKey(key);
    await saveKey({ ...key, name: "after" });

    const keys = await listKeys();
    const matches = keys.filter((k) => k.id === key.id);
    expect(matches).toHaveLength(1);
    expect(matches[0].name).toBe("after");
    await deleteKey(key.id);
  });
});
