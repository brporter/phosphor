// End-to-end encryption using Web Crypto API.
// Matches internal/crypto/crypto.go: PBKDF2-SHA256, AES-256-GCM, 12-byte nonce.

const ITERATIONS = 100000;
const KEY_LENGTH = 256; // bits
const NONCE_SIZE = 12; // bytes

export async function deriveKey(
  passphrase: string,
  saltBase64: string
): Promise<CryptoKey> {
  const salt = Uint8Array.from(atob(saltBase64), (c) => c.charCodeAt(0));
  const keyMaterial = await crypto.subtle.importKey(
    "raw",
    new TextEncoder().encode(passphrase),
    "PBKDF2",
    false,
    ["deriveKey"]
  );
  return crypto.subtle.deriveKey(
    {
      name: "PBKDF2",
      salt,
      iterations: ITERATIONS,
      hash: "SHA-256",
    },
    keyMaterial,
    { name: "AES-GCM", length: KEY_LENGTH },
    true, // extractable — needed to export for sessionStorage caching
    ["encrypt", "decrypt"]
  );
}

// Export a CryptoKey's raw bytes as a base64 string for sessionStorage caching.
// This stores the derived key (session-specific, not reusable) rather than the passphrase.
export async function exportKeyToBase64(key: CryptoKey): Promise<string> {
  const raw = await crypto.subtle.exportKey("raw", key);
  return btoa(String.fromCharCode(...new Uint8Array(raw)));
}

// Import raw key bytes from a base64 string (from sessionStorage cache).
// The imported key is non-extractable — it can only be used for encrypt/decrypt.
export async function importKeyFromBase64(base64: string): Promise<CryptoKey> {
  const raw = Uint8Array.from(atob(base64), (c) => c.charCodeAt(0));
  return crypto.subtle.importKey(
    "raw",
    raw,
    { name: "AES-GCM" },
    false, // non-extractable once imported from cache
    ["encrypt", "decrypt"]
  );
}

// Decrypt data produced by Go's Encrypt: [12-byte nonce][ciphertext + 16-byte GCM tag]
export async function decrypt(
  key: CryptoKey,
  data: Uint8Array
): Promise<Uint8Array> {
  const iv = data.slice(0, NONCE_SIZE);
  const ciphertext = data.slice(NONCE_SIZE);
  const plaintext = await crypto.subtle.decrypt(
    { name: "AES-GCM", iv },
    key,
    ciphertext
  );
  return new Uint8Array(plaintext);
}

// Encrypt data matching Go's format: [12-byte nonce][ciphertext + 16-byte GCM tag]
export async function encrypt(
  key: CryptoKey,
  data: Uint8Array
): Promise<Uint8Array> {
  const iv = crypto.getRandomValues(new Uint8Array(NONCE_SIZE));
  const ciphertext = await crypto.subtle.encrypt(
    { name: "AES-GCM", iv },
    key,
    data as unknown as ArrayBuffer
  );
  const result = new Uint8Array(NONCE_SIZE + ciphertext.byteLength);
  result.set(iv, 0);
  result.set(new Uint8Array(ciphertext as ArrayBuffer), NONCE_SIZE);
  return result;
}
