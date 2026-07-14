import "@testing-library/jest-dom/vitest";

// Node 22+ defines an experimental globalThis.localStorage (undefined unless
// node runs with --localstorage-file), and vitest's jsdom environment does not
// copy a jsdom global over a key that already exists on the Node global. That
// leaves localStorage/sessionStorage undefined in tests, so provide in-memory
// implementations of the Storage interface here.
class MemoryStorage implements Storage {
  private store = new Map<string, string>();

  get length(): number {
    return this.store.size;
  }

  clear(): void {
    this.store.clear();
  }

  getItem(key: string): string | null {
    return this.store.get(key) ?? null;
  }

  key(index: number): string | null {
    return [...this.store.keys()][index] ?? null;
  }

  removeItem(key: string): void {
    this.store.delete(key);
  }

  setItem(key: string, value: string): void {
    this.store.set(key, String(value));
  }
}

for (const name of ["localStorage", "sessionStorage"] as const) {
  if (typeof globalThis[name] === "undefined") {
    Object.defineProperty(globalThis, name, {
      value: new MemoryStorage(),
      writable: true,
      configurable: true,
    });
  }
}

// jsdom has no ResizeObserver (used by ConnectView's terminal fit logic).
if (typeof globalThis.ResizeObserver === "undefined") {
  globalThis.ResizeObserver = class ResizeObserver {
    observe(): void {}
    unobserve(): void {}
    disconnect(): void {}
  };
}
