import { useCallback, useEffect, useState } from "react";
import { listKeys, saveKey, deleteKey, type StoredKey } from "../lib/keys";
import { loadSSH } from "../lib/wasm";

export function KeysPage() {
  const [keys, setKeys] = useState<StoredKey[]>([]);
  const [name, setName] = useState("");
  const [passphrase, setPassphrase] = useState("");
  const [importPem, setImportPem] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [copied, setCopied] = useState<string | null>(null);

  const refresh = useCallback(() => {
    void listKeys().then(setKeys);
  }, []);

  useEffect(refresh, [refresh]);

  const handleGenerate = async () => {
    if (!name) return;
    setBusy(true);
    setError(null);
    try {
      const ssh = await loadSSH();
      const kp = ssh.generateKeypair(passphrase || undefined);
      const key: StoredKey = {
        id: crypto.randomUUID(),
        name,
        privateKeyPem: kp.privateKeyPem,
        authorizedKey: kp.authorizedKey,
        fingerprint: kp.fingerprint,
        createdAt: Date.now(),
        encrypted: passphrase !== "",
      };
      await saveKey(key);
      setName("");
      setPassphrase("");
      refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "key generation failed");
    } finally {
      setBusy(false);
    }
  };

  const handleImport = async () => {
    if (!name || !importPem) return;
    setBusy(true);
    setError(null);
    try {
      const ssh = await loadSSH();
      const info = ssh.publicKeyFromPem(importPem, passphrase || undefined);
      const key: StoredKey = {
        id: crypto.randomUUID(),
        name,
        privateKeyPem: importPem,
        authorizedKey: info.authorizedKey,
        fingerprint: info.fingerprint,
        createdAt: Date.now(),
        encrypted: passphrase !== "",
      };
      await saveKey(key);
      setName("");
      setPassphrase("");
      setImportPem("");
      refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "import failed (wrong passphrase?)");
    } finally {
      setBusy(false);
    }
  };

  const copyAuthorizedKey = async (key: StoredKey) => {
    await navigator.clipboard.writeText(key.authorizedKey);
    setCopied(key.id);
    setTimeout(() => setCopied(null), 2000);
  };

  const inputStyle: React.CSSProperties = {
    background: "#0a0a0a",
    border: "1px solid var(--border-crt)",
    color: "var(--green)",
    padding: "6px 10px",
    fontFamily: "inherit",
    fontSize: 13,
    outline: "none",
    width: "100%",
    boxSizing: "border-box",
  };

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 24, maxWidth: 640 }}>
      <div className="section-heading">// SSH KEYS</div>
      <p style={{ fontSize: 12, color: "var(--text-dim)", lineHeight: 1.6 }}>
        Keys are generated and stored in your browser — private keys never leave this device
        and the relay never sees them. To use a key, copy its{" "}
        <code style={{ color: "var(--green)" }}>authorized_keys</code> line onto the target
        machine's <code style={{ color: "var(--green)" }}>~/.ssh/authorized_keys</code>.
      </p>

      {error && <div style={{ color: "var(--red)", fontSize: 12 }}>{error}</div>}

      <div style={{ display: "flex", flexDirection: "column", gap: 10, border: "1px solid var(--border-crt)", padding: 16 }}>
        <div style={{ color: "var(--cyan, #00e5ff)", fontSize: 13, fontWeight: "bold" }}>// NEW KEY</div>
        <input value={name} onChange={(e) => setName(e.target.value)} placeholder="Key name (e.g. laptop)" style={inputStyle} />
        <input
          type="password"
          value={passphrase}
          onChange={(e) => setPassphrase(e.target.value)}
          placeholder="Passphrase (optional, encrypts the private key)"
          style={inputStyle}
        />
        <div style={{ display: "flex", gap: 8 }}>
          <button className="btn-action" disabled={busy || !name} onClick={handleGenerate}>
            [generate ed25519]
          </button>
        </div>
        <details>
          <summary style={{ fontSize: 12, color: "var(--text-dim)", cursor: "pointer" }}>
            or import an existing private key
          </summary>
          <div style={{ display: "flex", flexDirection: "column", gap: 10, marginTop: 10 }}>
            <textarea
              value={importPem}
              onChange={(e) => setImportPem(e.target.value)}
              placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
              rows={5}
              style={{ ...inputStyle, resize: "vertical", fontFamily: "inherit" }}
            />
            <button className="btn-action" disabled={busy || !name || !importPem} onClick={handleImport}>
              [import]
            </button>
          </div>
        </details>
      </div>

      <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
        {keys.length === 0 && (
          <div style={{ fontSize: 12, color: "var(--text-dim)" }}>No keys yet.</div>
        )}
        {keys.map((k) => (
          <div key={k.id} className="card-crt" style={{ display: "flex", alignItems: "center", gap: 12 }}>
            <div style={{ flex: 1 }}>
              <div style={{ color: "var(--green)", fontWeight: 600, fontSize: 13 }}>
                {k.name} {k.encrypted && <span className="badge badge-cyan">[encrypted]</span>}
              </div>
              <div style={{ fontSize: 11, color: "var(--text-dim)", wordBreak: "break-all" }}>
                {k.fingerprint}
              </div>
            </div>
            <button className="btn-action" onClick={() => copyAuthorizedKey(k)}>
              {copied === k.id ? "[copied!]" : "[copy pubkey]"}
            </button>
            <button
              className="btn-danger"
              onClick={() => {
                void deleteKey(k.id).then(refresh);
              }}
            >
              [delete]
            </button>
          </div>
        ))}
      </div>
    </div>
  );
}
