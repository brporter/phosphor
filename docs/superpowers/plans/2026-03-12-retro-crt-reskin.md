# Retro CRT Reskin Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reskin the phosphor web SPA with a cohesive Retro CRT aesthetic — green-tinted backgrounds, `[bracket]` labels, `// comment` headings, text-shadow glows — while establishing a shared CSS class system.

**Architecture:** Add CSS variables and reusable classes to `web/src/index.css`, then update each React component to use those classes, removing inline styles. No structural or behavioral changes.

**Tech Stack:** React 19, CSS (no framework), TypeScript, xterm.js

**Spec:** `docs/superpowers/specs/2026-03-12-retro-crt-reskin-design.md`

---

## File Map

| File | Action | Responsibility |
|---|---|---|
| `web/src/index.css` | Modify | Add CRT variables, badge/button/card/heading classes, update globals |
| `web/src/components/SessionCard.tsx` | Modify | Apply `.card-crt`, `.badge-*`, `.btn-danger`, bracket text, remove inline styles |
| `web/src/components/SessionList.tsx` | Modify | Apply `.section-heading`, CRT styling to loading/error/empty states |
| `web/src/components/TerminalView.tsx` | Modify | Apply `.status-bar`, `.badge-*`, `.btn-action`, update xterm theme |
| `web/src/components/Layout.tsx` | Modify | CRT header/footer, `>_ phosphor` logo, `.btn-action` logout |
| `web/src/components/ProtectedRoute.tsx` | Modify | Stronger ASCII art glow, `// AUTHENTICATION REQUIRED` heading |
| `web/src/components/ProviderButtons.tsx` | Modify | Bracket labels, `.btn-primary`/`.btn-action` classes |
| `web/src/components/SettingsPage.tsx` | Modify | `.section-heading`, CRT card styling, bracket button labels |

---

## Chunk 1: CSS Foundation

### Task 1: Add CRT CSS variables and classes to index.css

**Files:**
- Modify: `web/src/index.css`

- [ ] **Step 1: Add CRT variables to `:root`**

Add after the existing `--font-mono` line:

```css
  --bg-crt: #050808;
  --bg-card-crt: #0a120a;
  --bg-panel-crt: #0a1a0a;
  --border-crt: #0a3a0a;
  --text-dim: #0a5a0a;
```

- [ ] **Step 2: Update global background and base styles**

Change `html, body, #root` background from `var(--bg)` to `var(--bg-crt)`.

Update the base `button` style:
```css
button {
  font-family: var(--font-mono);
  cursor: pointer;
  border: 1px solid var(--border-crt);
  background: var(--bg-card-crt);
  color: var(--text-bright);
  padding: 8px 16px;
  font-size: 13px;
  transition: all 0.15s;
}
button:hover {
  border-color: #0a5a0a;
  box-shadow: 0 0 8px rgba(0, 255, 65, 0.15);
}
```

Update scrollbar thumb:
```css
::-webkit-scrollbar-thumb {
  background: var(--border-crt);
  border-radius: 3px;
}
```

Update scrollbar track:
```css
::-webkit-scrollbar-track {
  background: var(--bg-crt);
}
```

Update scanline overlay:
```css
body::after {
  content: "";
  position: fixed;
  inset: 0;
  background: repeating-linear-gradient(
    0deg,
    transparent,
    transparent 2px,
    rgba(0, 255, 65, 0.015) 2px,
    rgba(0, 255, 65, 0.015) 4px
  );
  pointer-events: none;
  z-index: 9999;
}
```

- [ ] **Step 3: Add badge classes**

Add after the existing `.btn-primary:hover` block:

```css
/* Badges */
.badge {
  display: inline-block;
  font-size: 10px;
  padding: 2px 8px;
  font-family: var(--font-mono);
}
.badge-green {
  color: var(--green);
  background: rgba(0, 255, 65, 0.08);
  text-shadow: 0 0 4px rgba(0, 255, 65, 0.3);
}
.badge-amber {
  color: var(--amber);
  background: rgba(255, 176, 0, 0.08);
  text-shadow: 0 0 4px rgba(255, 176, 0, 0.3);
}
.badge-red {
  color: var(--red);
  background: rgba(255, 51, 51, 0.08);
  text-shadow: 0 0 4px rgba(255, 51, 51, 0.3);
}
```

- [ ] **Step 4: Add button variant classes**

```css
/* Action buttons */
.btn-action {
  font-family: var(--font-mono);
  font-size: 10px;
  padding: 3px 10px;
  background: transparent;
  border: none;
  color: #00aa33;
  text-shadow: 0 0 4px rgba(0, 255, 65, 0.3);
  cursor: pointer;
  transition: all 0.15s;
}
.btn-action:hover {
  color: var(--green);
  text-shadow: 0 0 8px rgba(0, 255, 65, 0.5);
  box-shadow: none;
}
.btn-danger {
  font-family: var(--font-mono);
  font-size: 10px;
  padding: 3px 10px;
  background: transparent;
  border: none;
  color: var(--red);
  text-shadow: 0 0 4px rgba(255, 51, 51, 0.3);
  cursor: pointer;
  transition: all 0.15s;
}
.btn-danger:hover {
  color: #ff6666;
  text-shadow: 0 0 8px rgba(255, 51, 51, 0.5);
  box-shadow: none;
}
```

Update `.btn-primary`:
```css
.btn-primary {
  font-family: var(--font-mono);
  font-size: 10px;
  padding: 3px 10px;
  background: rgba(0, 255, 65, 0.08);
  border: none;
  color: var(--green);
  text-shadow: 0 0 4px rgba(0, 255, 65, 0.3);
  cursor: pointer;
  transition: all 0.15s;
}
.btn-primary:hover {
  background: rgba(0, 255, 65, 0.15);
  text-shadow: 0 0 8px rgba(0, 255, 65, 0.5);
  box-shadow: none;
}
```

- [ ] **Step 5: Add card, status-bar, and section-heading classes**

```css
/* CRT Card */
.card-crt {
  background: var(--bg-card-crt);
  border: 1px solid var(--border-crt);
  box-shadow: inset 0 0 20px rgba(0, 255, 65, 0.02);
  padding: 10px 14px;
  transition: all 0.15s;
}
.card-crt:hover {
  border-color: #0a5a0a;
  box-shadow: inset 0 0 20px rgba(0, 255, 65, 0.02), 0 0 12px rgba(0, 255, 65, 0.08);
}

/* Status bar */
.status-bar {
  background: var(--bg-panel-crt);
  border: 1px solid var(--border-crt);
  padding: 6px 10px;
  font-size: 11px;
}

/* Section heading */
.section-heading {
  font-size: 11px;
  color: var(--green);
  text-shadow: 0 0 6px rgba(0, 255, 65, 0.4);
  letter-spacing: 1px;
}
```

- [ ] **Step 6: Verify build**

Run: `cd web && npm run build`
Expected: Build succeeds with no errors.

- [ ] **Step 7: Commit**

```
git add web/src/index.css
git commit -m "feat(web): add CRT CSS variables and class system"
```

---

## Chunk 2: Session Cards and List

### Task 2: Restyle SessionCard

**Files:**
- Modify: `web/src/components/SessionCard.tsx`

- [ ] **Step 1: Rewrite SessionCard component**

Replace the entire component JSX (the `return` block) with the CRT version:

- Outer wrapper: `<div className="card-crt" style={{ display: "flex" }}>` — remove `onMouseEnter`/`onMouseLeave` handlers. Note: `.card-crt` provides `padding: 10px 14px`, so the outer div handles all padding.
- Inner `<Link>`: keep `to`, `style` simplified to `{ display: "block", flex: 1, textDecoration: "none" }` — NO padding on Link (card-crt handles it)
- Title row: `display: flex; align-items: center; gap: 10px` — no more `space-between`
  - Title span: `color: var(--green)`, `fontWeight: 600`, `fontSize: 13`, `textShadow: 0 0 6px rgba(0,255,65,0.3)`
  - Mode badge: `<span className="badge badge-amber">[{session.mode}]</span>`
  - Ready badge (conditional): `<span className="badge badge-green">[ready]</span>`
  - Exited badge (conditional): `<span className="badge badge-red">[exited]</span>`
- Metadata row: `fontSize: 11`, `color: var(--text-dim)`, `display: flex; gap: 12px`
  - `viewers: N` format
  - `id: xxx` format
- Destroy button: `<button className="btn-danger" onClick={...}>[destroy]</button>`

- [ ] **Step 2: Restyle destroy modal**

Within the same file, update the modal JSX:
- Overlay: `background: rgba(0, 0, 0, 0.85)`
- Modal container: `background: var(--bg-card-crt)`, `border: 1px solid var(--border-crt)`, `box-shadow: inset 0 0 20px rgba(0, 255, 65, 0.02)`
- Title: `// DESTROY SESSION` in `color: var(--red)`, `textShadow: 0 0 6px rgba(255,51,51,0.4)`, `fontWeight: bold`, `fontSize: 14`
- Warning text: `color: var(--text)`, `fontSize: 13`
- Cancel button: `className="btn-action"`, text: `[cancel]`
- Confirm button: `className="btn-danger"`, text: `[destroy session]`

- [ ] **Step 3: Verify build**

Run: `cd web && npm run build`
Expected: Build succeeds.

- [ ] **Step 4: Commit**

```
git add web/src/components/SessionCard.tsx
git commit -m "feat(web): restyle SessionCard with CRT theme"
```

### Task 3: Restyle SessionList

**Files:**
- Modify: `web/src/components/SessionList.tsx`

- [ ] **Step 1: Update SessionList component**

- Loading state: `color: var(--green)`, add `textShadow: 0 0 6px rgba(0,255,65,0.4)`, `padding: 16`
- Error state: `color: var(--red)`, add `textShadow: 0 0 6px rgba(255,51,51,0.4)`, `padding: 16`
- Empty state: add `textShadow: 0 0 6px rgba(0,255,65,0.4)` to `<pre>`, update code block to use `background: var(--bg-card-crt)`, `border: 1px solid var(--border-crt)`
- Heading: replace `<h2>` with `<div className="section-heading">// ACTIVE SESSIONS =============== [{sessions.length}]</div>`, `marginBottom: 16`
- Session list gap: change from `8` to `6`

- [ ] **Step 2: Verify build**

Run: `cd web && npm run build`
Expected: Build succeeds.

- [ ] **Step 3: Commit**

```
git add web/src/components/SessionList.tsx
git commit -m "feat(web): restyle SessionList with CRT theme"
```

---

## Chunk 3: Terminal View

### Task 4: Restyle TerminalView

**Files:**
- Modify: `web/src/components/TerminalView.tsx`

- [ ] **Step 1: Update status bar**

- Wrap status bar in `className="status-bar"` div, update to use `display: flex; justify-content: space-between; align-items: center`
- Back link: `color: #00aa33`
- Command span: `color: var(--amber)`, add `textShadow: 0 0 4px rgba(255,176,0,0.3)`
- Connection status badges:
  - Process exited: `<span className="badge badge-amber">[exited ({processExited})]</span>` + `<button className="btn-action" onClick={sendRestart}>[restart]</button>`
  - Ended (maps to both `ended` state and generic disconnection): `<span className="badge badge-red">[disconnected]</span>`
  - Connected: `<span className="badge badge-green">[connected]</span>`
  - Error: `<span className="badge badge-red">[{error}]</span>`
  - Connecting: `<span className="badge badge-amber">[connecting...]</span>`

- [ ] **Step 2: Update terminal container and xterm theme**

- Terminal container border: `1px solid var(--border-crt)`
- Terminal container background: `#050808`
- xterm.js theme: change `background: "#0a0a0a"` to `background: "#050808"`
- Pipe mode indicator: `color: var(--text-dim)`

- [ ] **Step 3: Verify build**

Run: `cd web && npm run build`
Expected: Build succeeds.

- [ ] **Step 4: Commit**

```
git add web/src/components/TerminalView.tsx
git commit -m "feat(web): restyle TerminalView with CRT theme"
```

---

## Chunk 4: Layout, Auth, and Settings

### Task 5: Restyle Layout (header/footer)

**Files:**
- Modify: `web/src/components/Layout.tsx`

- [ ] **Step 1: Update header**

- Header: `background: var(--bg-panel-crt)`, `borderBottom: 1px solid var(--border-crt)`
- Logo text: change `{">"} phosphor` to `{">_"} phosphor`
- Logo style: `textShadow: 0 0 12px rgba(0,255,65,0.5)`
- User email: `color: #00aa33`
- Releases link: `color: #00aa33`, `textDecoration: none`
- Settings link: `color: #00aa33`, `textDecoration: none`
- Logout button: `className="btn-action"`, text: `[logout]`

- [ ] **Step 2: Update footer**

- Footer: `color: var(--text-dim)`, `borderTop: 1px solid var(--border-crt)`

- [ ] **Step 3: Update main area**

- Main: `background: var(--bg-crt)` (or inherit from body — it should already work)

- [ ] **Step 4: Verify build**

Run: `cd web && npm run build`
Expected: Build succeeds.

- [ ] **Step 5: Commit**

```
git add web/src/components/Layout.tsx
git commit -m "feat(web): restyle Layout header/footer with CRT theme"
```

### Task 6: Restyle ProtectedRoute

**Files:**
- Modify: `web/src/components/ProtectedRoute.tsx`

- [ ] **Step 1: Update login screen**

- Loading "Initializing...": keep green, add `textShadow: 0 0 6px rgba(0, 255, 65, 0.4)`
- ASCII art `<pre>`: `textShadow: 0 0 12px rgba(0,255,65,0.5)`
- Change `<p>Sign in to view your terminal sessions</p>` to `<div className="section-heading">// AUTHENTICATION REQUIRED</div>`

- [ ] **Step 2: Verify build**

Run: `cd web && npm run build`
Expected: Build succeeds.

- [ ] **Step 3: Commit**

```
git add web/src/components/ProtectedRoute.tsx
git commit -m "feat(web): restyle login screen with CRT theme"
```

### Task 7: Restyle ProviderButtons

**Files:**
- Modify: `web/src/components/ProviderButtons.tsx`

- [ ] **Step 1: Update provider buttons**

- Update `PROVIDER_LABELS` to bracket notation: `"[sign in with Microsoft]"`, `"[sign in with Google]"`, `"[sign in with Apple]"`, `"[dev mode]"`
- First button: `className="btn-primary"`
- Other buttons: `className="btn-action"`
- Container layout: keep existing `display: flex; gap: 12px` (already matches spec)

- [ ] **Step 2: Verify build**

Run: `cd web && npm run build`
Expected: Build succeeds.

- [ ] **Step 3: Commit**

```
git add web/src/components/ProviderButtons.tsx
git commit -m "feat(web): restyle ProviderButtons with CRT theme"
```

### Task 8: Restyle SettingsPage

**Files:**
- Modify: `web/src/components/SettingsPage.tsx`

- [ ] **Step 1: Update settings page**

- Page heading: `<div className="section-heading" style={{ marginBottom: 24 }}>// SETTINGS</div>`
- Section heading: `<div className="section-heading" style={{ marginBottom: 12 }}>// API KEYS</div>`
- Description: `color: var(--text)`, `fontSize: 13`
- Generate button: `className="btn-primary"`, text: `[generate API key]` / `[generating...]`
- API key display: `background: var(--bg-card-crt)`, `border: 1px solid var(--border-crt)`, `color: var(--green)`, add `textShadow: 0 0 4px rgba(0,255,65,0.3)`
- Copy button: `className="btn-primary"`, text: `[copy to clipboard]` (current text is "copy to clipboard", just adding brackets)
- Warning text: `color: var(--amber)`, add `textShadow: 0 0 4px rgba(255,176,0,0.3)`
- Pre block: `background: var(--bg-card-crt)`, `border: 1px solid var(--border-crt)`

- [ ] **Step 2: Verify build**

Run: `cd web && npm run build`
Expected: Build succeeds.

- [ ] **Step 3: Commit**

```
git add web/src/components/SettingsPage.tsx
git commit -m "feat(web): restyle SettingsPage with CRT theme"
```

---

## Chunk 5: Final Verification

### Task 9: Full build and visual check

- [ ] **Step 1: Full build**

Run: `cd web && npm run build`
Expected: Build succeeds with no errors or warnings.

- [ ] **Step 2: Start dev server and verify visually**

Run relay: `go run ./cmd/relay` (terminal 1)
Run web: `cd web && npm run dev` (terminal 2)

Check each page:
- Login screen: green ASCII art with glow, `// AUTHENTICATION REQUIRED`, bracket sign-in buttons
- Session list (empty): ASCII prompt with CRT styling
- Session list (with sessions): `// ACTIVE SESSIONS` heading, `.card-crt` cards with badges
- Terminal view: `.status-bar` with badge status indicators
- Settings: `// SETTINGS` and `// API KEYS` headings, CRT-styled key display

- [ ] **Step 3: Final commit (if any fixups needed)**

```
git add -A web/src/
git commit -m "fix(web): CRT reskin polish"
```
