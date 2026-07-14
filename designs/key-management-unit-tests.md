# Unit Test Plan: Key Management & WASM Error Contract

**Status:** implemented (2026-07-14)
**Date:** 2026-07-14

Implementation notes (deviations from the original proposal):
- `wasm.ts` gained an exported `wrapPhosphorSSH(raw)` so the production wrapper is unit-tested directly instead of testing a private `unwrap`.
- The Layer B contract test is self-contained: it derives encrypted/unencrypted PEMs from the module's own `generateKeypair` rather than checked-in key fixtures.
- The AuthProvider baseline failure was Node 22+'s experimental `localStorage` global shadowing jsdom's (vitest only copies jsdom globals not already present on the Node global); fixed with an in-memory Storage polyfill in `src/test/setup.ts`, which also stubs `ResizeObserver`.
**Motivating bugs:** the import-key flow silently did nothing (import button disabled with no visual feedback when the key name was empty), and WASM key functions returned `Error` objects instead of throwing — so parse failures were swallowed, corrupt key records were saved to IndexedDB, and the UI `try/catch` was dead code.

The fixes introduced: an `{ok, error}` envelope in `internal/webssh/register.go` unwrapped (re-thrown) in `web/src/lib/wasm.ts`; an `ImportKeyModal` with its own name field; an inline session-only key at connect time (`SessionKey` in `useSSH`); and `:disabled` button styling. This plan covers tests that pin each of those behaviors so they cannot silently regress.

## Test architecture — three layers

The failure that motivated this plan lived at a **boundary** (Go/WASM ↔ TypeScript) where neither `go test` nor `tsc` can see the other side. The layers below each own one slice, and Layer B pins the boundary itself.

### Layer A — Go key-derivation logic (`go test`)

`register.go` is build-tagged `js && wasm`, so it cannot be tested with plain `go test` today. **Refactor:** extract the pure logic into a new build-tag-free package, leaving `register.go` as a thin `js.Value` adapter:

```
internal/sshkeys/sshkeys.go
  GenerateKeypair(passphrase string) (Keypair, error)
  PublicKeyFromPem(pem, passphrase string) (PublicKeyInfo, error)
```

`internal/sshkeys/sshkeys_test.go` cases (table-driven; generate fixtures in-test with `x/crypto/ssh`, no fixture files):

| Case | Expected |
|---|---|
| valid unencrypted ed25519 PEM | authorizedKey + `SHA256:` fingerprint, no error |
| valid RSA PEM (PKCS#1) | success — import is not ed25519-only |
| encrypted PEM + correct passphrase | success |
| encrypted PEM + empty passphrase | error mentioning passphrase protection |
| encrypted PEM + wrong passphrase | decryption error |
| unencrypted PEM + spurious passphrase | error (not silent success) |
| garbage / empty input | `ssh: no key found`-class error |
| PEM with surrounding whitespace / missing trailing newline | success (paste tolerance) |
| generate with passphrase | round-trips: `PublicKeyFromPem(generated.PEM, passphrase)` matches fingerprint |

These run in the ordinary `go test ./... -count=1` invocation with no wasm toolchain.

### Layer B — WASM ↔ JS envelope contract (vitest, node environment)

Pins the boundary that type-checking cannot see: the shape the Go module actually returns to JS. A drift here (e.g. someone renames `ok` → `success` on one side) breaks the app while both `go test` and `tsc` stay green.

`web/src/lib/wasm.contract.test.ts`, annotated `// @vitest-environment node`:

- Load `web/public/wasm_exec.js` + `web/public/phosphor-ssh.wasm` exactly as the harness does (this was already validated manually during the fix).
- `describe.skipIf(!wasmExists)` so the suite skips (loudly) when the artifact isn't built; CI runs `make wasm` first so it never skips there.
- Assertions on the **raw global**, not the wrapper:
  - success → `{ok: true, authorizedKey: /^ssh-/, fingerprint: /^SHA256:/}` and **no** `error` key;
  - failure (garbage, encrypted-no-passphrase, wrong passphrase) → `{ok: false, error: <non-empty string>}` and no key fields;
  - result is a plain object, **not** `instanceof Error` (the old bug shape);
  - `generateKeypair` envelope has the same invariants plus `privateKeyPem`.

### Layer C — TypeScript unit/component tests (vitest, jsdom — existing setup)

Mock `../lib/wasm` and `../lib/keys` with `vi.mock`; no real WASM in this layer.

**`web/src/lib/wasm.test.ts` — `unwrap` semantics via `loadSSH`:**
- Install a fake `window.phosphorSSH` returning `{ok: false, error: "ssh: no key found"}` → wrapper **throws** `Error("ssh: no key found")`. This is the direct regression test for the swallowed-error bug.
- `{ok: true, ...fields}` → returns the fields.
- `connect` passes through untouched (promise semantics unchanged).

**`web/src/components/ImportKeyModal.test.tsx`:**
- import button `disabled` until name **and** PEM are non-whitespace; enabled after both.
- success path: `saveKey` called with `authorizedKey`/`fingerprint` from the (mocked) WASM result, `encrypted` reflecting passphrase presence; `onImported` fires.
- failure path (mock throws): error text rendered, **`saveKey` not called**, modal stays open — the corrupt-record regression.
- `[cancel]` and overlay click call `onClose`; panel click does not.

**`web/src/components/KeysPage.test.tsx`:**
- `[import existing key]` opens the modal; import success closes it and refreshes the list.
- generate failure (mock throws) → error shown, nothing saved; generate success → list refreshes, inputs clear.
- `[generate ed25519]` disabled with empty name — and asserted via the `disabled` attribute, which is what the new `:disabled` CSS keys off.

**`web/src/components/ConnectView.test.tsx`** (render under `MemoryRouter` with a mocked `useSSH` capturing its options, or a mocked `../lib/wasm`):
- selecting "Use a key just for this session…" reveals PEM textarea, file loader, passphrase input.
- `[connect]` disabled while inline is selected and PEM is empty; enabled once pasted.
- invalid inline PEM (mock `publicKeyFromPem` throws) → error shown, session **not** started.
- valid inline PEM → `useSSH` receives `key: {privateKeyPem, passphrase}`; **`saveKey` is never called** (on-demand keys must not persist).
- stored-key selection still maps to `{privateKeyPem}` from IndexedDB-backed list.
- file input populates the textarea (`File.text()` mock).
- re-render with unchanged form state passes a **referentially stable** `key` to `useSSH` (the `useMemo` guard — prevents session teardown churn).

**`web/src/hooks/useSSH.test.ts`:**
- `ssh.connect` receives `privateKey` and `keyPassphrase` from the `SessionKey`.
- key identity change before connect does not double-connect (`startedRef` guard).

**Button convention guard (`web/src/test/buttonStyling.test.tsx`):**
- For each rendered page/component above, assert every `<button>` has one of `btn-action | btn-danger | btn-primary` (test-library `getAllByRole("button")` sweep). Keeps the "consistent button classes" review from decaying.
- Note: the *visual* rendering of `:disabled` is CSS and not meaningful in jsdom; the invariant unit tests protect is "predicate unmet ⇒ `disabled` attribute present", and the Playwright e2e suite (`npm run test:e2e`) is the right home for a visual/interaction spot-check of the disabled look if desired.

## Regression → test mapping

| Original bug | Pinned by |
|---|---|
| WASM returned `Error` instead of throwing; UI catch dead | Layer B raw-envelope invariants; `wasm.test.ts` unwrap-throws |
| Corrupt key saved on parse failure | ImportKeyModal failure path (`saveKey` not called) |
| Import silently no-ops without a name | ImportKeyModal disabled-predicate + name field co-located test |
| Disabled buttons indistinguishable | `disabled`-attribute assertions + button convention guard (visual in e2e) |
| Encrypted-key passphrase never forwarded to connect | `useSSH.test.ts` keyPassphrase pass-through |
| Inline key accidentally persisted | ConnectView "saveKey never called" |

## Prerequisites & CI wiring

1. **Fix the pre-existing `AuthProvider.test.tsx` failures first** (8 tests failing on `main`: `localStorage` is `undefined` in setup — a vitest/jsdom environment issue unrelated to key management). A red baseline hides new regressions.
2. Add dev-dependency `fake-indexeddb` if we choose to test `lib/keys.ts` against a real IndexedDB shim rather than mocking it (recommended: one small `keys.test.ts` for save/list/delete round-trip).
3. CI order: `go test ./...` (includes new `internal/sshkeys`) → `make wasm` → `npm test` (Layer B now has the artifact) → existing build steps.

## Out of scope

- Full SSH connect-path testing (needs a live sshd + relay; covered by the Playwright e2e suite).
- Relay/store Go tests (unchanged by this work).
