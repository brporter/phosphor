// Loads the Go-compiled WASM SSH client and exposes its `phosphorSSH` global.
// The module is fetched lazily on first connect so the SPA's initial load is
// not burdened with a multi-megabyte download.

export interface HostKeyPrompt {
  fingerprint: string;
  keyType: string;
}

export interface SSHCallbacks {
  onData?: (data: Uint8Array) => void;
  onClose?: () => void;
  onPassword?: () => Promise<string>;
  onKeyboardInteractive?: (
    name: string,
    instruction: string,
    questions: string[],
  ) => Promise<string[]>;
  onHostKey?: (fingerprint: string, keyType: string) => Promise<boolean>;
}

export interface ConnectOptions {
  wsURL: string;
  token: string | null;
  username: string;
  privateKey?: string;
  keyPassphrase?: string;
  rows?: number;
  cols?: number;
  callbacks: SSHCallbacks;
}

export interface SSHHandle {
  write(data: Uint8Array | string): void;
  resize(cols: number, rows: number): void;
  disconnect(): void;
}

export interface GeneratedKeypair {
  privateKeyPem: string;
  authorizedKey: string;
  fingerprint: string;
}

export interface PublicKeyInfo {
  authorizedKey: string;
  fingerprint: string;
}

// Synchronous WASM calls return an {ok, ...} envelope instead of throwing —
// a js.FuncOf callback in Go cannot raise a JS exception. unwrap() converts
// the failure arm back into a thrown Error so callers can use try/catch.
export type WasmResult<T> = ({ ok: true } & T) | { ok: false; error: string };

export interface RawPhosphorSSH {
  connect(opts: ConnectOptions): Promise<SSHHandle>;
  generateKeypair(passphrase?: string): WasmResult<GeneratedKeypair>;
  publicKeyFromPem(pem: string, passphrase?: string): WasmResult<PublicKeyInfo>;
}

export interface PhosphorSSH {
  connect(opts: ConnectOptions): Promise<SSHHandle>;
  generateKeypair(passphrase?: string): GeneratedKeypair;
  publicKeyFromPem(pem: string, passphrase?: string): PublicKeyInfo;
}

function unwrap<T>(result: WasmResult<T>): T {
  if (!result.ok) throw new Error(result.error);
  return result;
}

// wrapPhosphorSSH converts the raw envelope-returning global into the
// exception-throwing API the app consumes. Exported for tests.
export function wrapPhosphorSSH(raw: RawPhosphorSSH): PhosphorSSH {
  return {
    connect: (opts) => raw.connect(opts),
    generateKeypair: (passphrase) => unwrap(raw.generateKeypair(passphrase)),
    publicKeyFromPem: (pem, passphrase) => unwrap(raw.publicKeyFromPem(pem, passphrase)),
  };
}

declare global {
  interface Window {
    Go: new () => {
      run(instance: WebAssembly.Instance): void;
      importObject: WebAssembly.Imports;
    };
    phosphorSSH?: RawPhosphorSSH;
  }
}

let loadPromise: Promise<PhosphorSSH> | null = null;

function loadScript(src: string): Promise<void> {
  return new Promise((resolve, reject) => {
    const existing = document.querySelector(`script[src="${src}"]`);
    if (existing) {
      resolve();
      return;
    }
    const script = document.createElement("script");
    script.src = src;
    script.onload = () => resolve();
    script.onerror = () => reject(new Error(`failed to load ${src}`));
    document.head.appendChild(script);
  });
}

// loadSSH returns the phosphorSSH API, instantiating the WASM module once.
export function loadSSH(): Promise<PhosphorSSH> {
  if (loadPromise) return loadPromise;

  loadPromise = (async () => {
    await loadScript("/wasm_exec.js");
    const go = new window.Go();
    const result = await WebAssembly.instantiateStreaming(
      fetch("/phosphor-ssh.wasm"),
      go.importObject,
    );
    // Do not await go.run — it blocks forever (the WASM main() is `select {}`).
    void go.run(result.instance);

    // Wait for the module to install its global.
    for (let i = 0; i < 100; i++) {
      const raw = window.phosphorSSH;
      if (raw) return wrapPhosphorSSH(raw);
      await new Promise((r) => setTimeout(r, 10));
    }
    throw new Error("phosphorSSH global did not initialize");
  })();

  return loadPromise;
}
