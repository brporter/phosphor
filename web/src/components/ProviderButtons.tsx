const PROVIDER_LABELS: Record<string, string> = {
  microsoft: "[sign in with Microsoft]",
  google: "[sign in with Google]",
  apple: "[sign in with Apple]",
  dev: "[dev mode]",
};

function providerLabel(provider: string): string {
  return PROVIDER_LABELS[provider] ?? `[sign in with ${provider}]`;
}

interface ProviderButtonsProps {
  providers: string[];
  login: (provider: string) => Promise<void>;
}

export function ProviderButtons({ providers, login }: ProviderButtonsProps) {
  return (
    <div style={{ display: "flex", gap: 12 }}>
      {providers.map((p, i) => (
        <button
          key={p}
          className={i === 0 ? "btn-primary" : "btn-action"}
          onClick={() => void login(p)}
        >
          {providerLabel(p)}
        </button>
      ))}
    </div>
  );
}
