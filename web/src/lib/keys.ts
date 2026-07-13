// Browser-side storage for SSH keypairs and host-key pins.
//
// Private keys live in IndexedDB and never leave the browser. Host-key pins
// (machine ID -> accepted fingerprint) are small and kept in localStorage for
// synchronous access during the SSH handshake.

export interface StoredKey {
  id: string;
  name: string;
  privateKeyPem: string;
  authorizedKey: string;
  fingerprint: string;
  createdAt: number;
  // Whether the PEM is passphrase-encrypted (decrypted in-WASM at use).
  encrypted: boolean;
}

const DB_NAME = "phosphor";
const DB_VERSION = 1;
const KEY_STORE = "keys";
const HOSTKEY_PREFIX = "phosphor_hostkey_";

function openDB(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const req = indexedDB.open(DB_NAME, DB_VERSION);
    req.onupgradeneeded = () => {
      const db = req.result;
      if (!db.objectStoreNames.contains(KEY_STORE)) {
        db.createObjectStore(KEY_STORE, { keyPath: "id" });
      }
    };
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error);
  });
}

export async function listKeys(): Promise<StoredKey[]> {
  const db = await openDB();
  return new Promise((resolve, reject) => {
    const tx = db.transaction(KEY_STORE, "readonly");
    const req = tx.objectStore(KEY_STORE).getAll();
    req.onsuccess = () => resolve(req.result as StoredKey[]);
    req.onerror = () => reject(req.error);
  });
}

export async function saveKey(key: StoredKey): Promise<void> {
  const db = await openDB();
  return new Promise((resolve, reject) => {
    const tx = db.transaction(KEY_STORE, "readwrite");
    tx.objectStore(KEY_STORE).put(key);
    tx.oncomplete = () => resolve();
    tx.onerror = () => reject(tx.error);
  });
}

export async function deleteKey(id: string): Promise<void> {
  const db = await openDB();
  return new Promise((resolve, reject) => {
    const tx = db.transaction(KEY_STORE, "readwrite");
    tx.objectStore(KEY_STORE).delete(id);
    tx.oncomplete = () => resolve();
    tx.onerror = () => reject(tx.error);
  });
}

// Host-key pins (trust-on-first-use).

export function getHostKeyPin(machineId: string): string | null {
  return localStorage.getItem(HOSTKEY_PREFIX + machineId);
}

export function setHostKeyPin(machineId: string, fingerprint: string): void {
  localStorage.setItem(HOSTKEY_PREFIX + machineId, fingerprint);
}

export function clearHostKeyPin(machineId: string): void {
  localStorage.removeItem(HOSTKEY_PREFIX + machineId);
}
