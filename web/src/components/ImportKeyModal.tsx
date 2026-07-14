import { useState } from "react";
import { saveKey, type StoredKey } from "../lib/keys";
import { loadSSH } from "../lib/wasm";

interface ImportKeyModalProps {
  onClose: () => void;
  onImported: (key: StoredKey) => void;
}

export function ImportKeyModal({ onClose, onImported }: ImportKeyModalProps) {
  const [name, setName] = useState("");
  const [pem, setPem] = useState("");
  const [passphrase, setPassphrase] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const canImport = !busy && name.trim() !== "" && pem.trim() !== "";

  const handleImport = async () => {
    if (!canImport) return;
    setBusy(true);
    setError(null);
    try {
      const ssh = await loadSSH();
      const info = ssh.publicKeyFromPem(pem, passphrase || undefined);
      const key: StoredKey = {
        id: crypto.randomUUID(),
        name: name.trim(),
        privateKeyPem: pem,
        authorizedKey: info.authorizedKey,
        fingerprint: info.fingerprint,
        createdAt: Date.now(),
        encrypted: passphrase !== "",
      };
      await saveKey(key);
      onImported(key);
    } catch (err) {
      setError(err instanceof Error ? err.message : "import failed (wrong passphrase?)");
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="modal-overlay" onClick={onClose}>
      <form
        className="modal-panel"
        onClick={(e) => e.stopPropagation()}
        onSubmit={(e) => {
          e.preventDefault();
          void handleImport();
        }}
      >
        <div style={{ color: "var(--cyan, #00e5ff)", fontWeight: "bold", fontSize: 14 }}>
          // IMPORT KEY
        </div>
        <label style={{ display: "flex", flexDirection: "column", gap: 6, fontSize: 12, color: "var(--text-dim)" }}>
          Key name
          <input
            className="input-crt"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="e.g. laptop"
            autoFocus
          />
        </label>
        <label style={{ display: "flex", flexDirection: "column", gap: 6, fontSize: 12, color: "var(--text-dim)" }}>
          Private key (PEM)
          <textarea
            className="input-crt"
            value={pem}
            onChange={(e) => setPem(e.target.value)}
            placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
            rows={6}
            style={{ resize: "vertical" }}
          />
        </label>
        <label style={{ display: "flex", flexDirection: "column", gap: 6, fontSize: 12, color: "var(--text-dim)" }}>
          Passphrase (only if the key is encrypted)
          <input
            className="input-crt"
            type="password"
            value={passphrase}
            onChange={(e) => setPassphrase(e.target.value)}
          />
        </label>
        {error && <div style={{ color: "var(--red)", fontSize: 12 }}>{error}</div>}
        <div style={{ display: "flex", gap: 12, justifyContent: "flex-end" }}>
          <button type="button" className="btn-action" onClick={onClose}>
            [cancel]
          </button>
          <button type="submit" className="btn-action" disabled={!canImport}>
            {busy ? "[importing...]" : "[import]"}
          </button>
        </div>
      </form>
    </div>
  );
}
