import { useState } from "react";
import { useAuth } from "../auth/useAuth";
import { generateApiKey } from "../lib/api";

export function SettingsPage() {
  const { getToken } = useAuth();
  const [apiKey, setApiKey] = useState<string | null>(null);
  const [keyId, setKeyId] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [generating, setGenerating] = useState(false);

  const handleGenerate = async () => {
    setError(null);
    setGenerating(true);
    try {
      const result = await generateApiKey(getToken());
      setApiKey(result.api_key);
      setKeyId(result.key_id);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to generate API key");
    } finally {
      setGenerating(false);
    }
  };

  const handleCopy = () => {
    if (apiKey) {
      navigator.clipboard.writeText(apiKey);
    }
  };

  return (
    <div style={{ maxWidth: 640 }}>
      <h2 style={{ color: "var(--green)", marginBottom: 24 }}>Settings</h2>

      <section>
        <h3 style={{ color: "var(--text)", marginBottom: 12 }}>API Keys</h3>
        <p style={{ color: "var(--text)", fontSize: 13, marginBottom: 16 }}>
          Generate an API key to authenticate the phosphor daemon. The key is
          shown once — copy it and store it securely.
        </p>

        {apiKey ? (
          <div>
            <div
              style={{
                background: "var(--bg-card)",
                border: "1px solid var(--border)",
                padding: 12,
                marginBottom: 8,
                wordBreak: "break-all",
                fontSize: 12,
                color: "var(--green)",
              }}
            >
              {apiKey}
            </div>
            <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
              <button className="btn-primary" onClick={handleCopy}>
                copy to clipboard
              </button>
              <span style={{ color: "var(--text)", fontSize: 12 }}>
                Key ID: {keyId}
              </span>
            </div>
            <p
              style={{
                color: "var(--amber)",
                fontSize: 12,
                marginTop: 12,
              }}
            >
              This key will not be shown again. Install it on your daemon with:
            </p>
            <pre
              style={{
                color: "var(--text)",
                fontSize: 12,
                background: "var(--bg-card)",
                border: "1px solid var(--border)",
                padding: 8,
                marginTop: 4,
              }}
            >
              sudo phosphor daemon set-key {apiKey}
            </pre>
          </div>
        ) : (
          <div>
            <button
              className="btn-primary"
              onClick={() => void handleGenerate()}
              disabled={generating}
            >
              {generating ? "generating..." : "generate API key"}
            </button>
            {error && (
              <p style={{ color: "var(--red)", fontSize: 12, marginTop: 8 }}>
                {error}
              </p>
            )}
          </div>
        )}
      </section>
    </div>
  );
}
