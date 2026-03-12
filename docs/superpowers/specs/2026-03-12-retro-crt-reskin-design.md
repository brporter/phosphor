# Retro CRT Reskin — Design Spec

## Overview

Redesign the phosphor web SPA to adopt a cohesive Retro CRT aesthetic. The current UI has inconsistent spacing, mixed inline styles, and no shared visual language for badges, buttons, and status indicators. This spec establishes a CSS class system and applies a green-tinted CRT theme across all components.

## Scope

This is a **visual-only** redesign. No structural, behavioral, or logic changes to any component. However, this does include:

- **Text content changes**: Button and badge labels gain bracket notation (e.g., `destroy` becomes `[destroy]`). Headings gain `//` comment-style prefixes. These are purely cosmetic text changes applied in JSX, not via CSS pseudo-elements.
- **Logo text change**: `> phosphor` becomes `>_ phosphor` (adding the underscore cursor character to reinforce the terminal metaphor).
- **Replacing inline JS hover handlers** with CSS `:hover` rules — same visual behavior, cleaner implementation.

**Not** in scope: new components, new pages, WebSocket/auth/API changes, responsive/mobile changes, new animations.

## Design Direction

"Retro CRT" — leans into the phosphor/CRT metaphor. Green-tinted backgrounds, text-shadow glow on interactive elements, `[bracket]`-wrapped labels, `// COMMENT` style headings, and a prominent scanline overlay. The overall feel is a mainframe console or vintage terminal monitor.

## Visual Language

### Color Palette (new CSS variables)

| Variable | Value | Purpose |
|---|---|---|
| `--bg-crt` | `#050808` | Page-level background (replaces `--bg`) |
| `--bg-card-crt` | `#0a120a` | Card/panel backgrounds |
| `--bg-panel-crt` | `#0a1a0a` | Header, footer, status bars |
| `--border-crt` | `#0a3a0a` | Green-tinted borders |
| `--text-dim` | `#0a5a0a` | Dim metadata text |

Existing variables (`--green`, `--green-dim`, `--green-glow`, `--amber`, `--red`, etc.) are retained unchanged.

### Badge System

All badges share a base `.badge` class:
- Font size: 10px
- Padding: `2px 8px`
- Content wrapped in brackets in JSX: `[pty]`, `[ready]`, `[exited]`
- Color-tinted `rgba` background, no solid border
- `text-shadow` glow matching the badge color
- `display: inline-block`

Variants:
- `.badge-green` — color: `var(--green)`, background: `rgba(0, 255, 65, 0.08)`, text-shadow: `0 0 4px rgba(0, 255, 65, 0.3)`
- `.badge-amber` — color: `var(--amber)`, background: `rgba(255, 176, 0, 0.08)`, text-shadow: `0 0 4px rgba(255, 176, 0, 0.3)`
- `.badge-red` — color: `var(--red)`, background: `rgba(255, 51, 51, 0.08)`, text-shadow: `0 0 4px rgba(255, 51, 51, 0.3)`

### Button System

Action buttons use `.btn-action`:
- Font family: `var(--font-mono)`
- Font size: 10px
- Padding: `3px 10px`
- Background: `transparent`
- Border: `none`
- Color: `#00aa33` (dim green)
- `text-shadow: 0 0 4px rgba(0, 255, 65, 0.3)`
- Cursor: pointer
- On hover: color brightens to `var(--green)`, text-shadow intensifies

Variants:
- `.btn-danger` — extends `.btn-action`. Color: `var(--red)`, text-shadow red-tinted. Hover: brighter red.
- `.btn-primary` — extends `.btn-action`. Color: `var(--green)`, background: `rgba(0, 255, 65, 0.08)`, text-shadow green-tinted. Hover: stronger background glow.
- `.btn-action` (base) — dim green as described above.

All action buttons use bracket notation in JSX: `[destroy]`, `[restart]`, `[logout]`, `[cancel]`, etc.

### Headings

`.section-heading` class:
- `// UPPERCASE TEXT` format with padding characters (`===`)
- Font size: 11px
- Color: `var(--green)`
- `text-shadow: 0 0 6px rgba(0, 255, 65, 0.4)`
- Letter-spacing: `1px`

### Card

`.card-crt` class:
- Background: `var(--bg-card-crt)`
- Border: `1px solid var(--border-crt)`
- `box-shadow: inset 0 0 20px rgba(0, 255, 65, 0.02)`
- Padding: `10px 14px`
- Transition: `all 0.15s`
- On `:hover`: border-color `#0a5a0a`, box-shadow adds outer glow `0 0 12px rgba(0, 255, 65, 0.08)`

This replaces the current JS `onMouseEnter`/`onMouseLeave` handlers on SessionCard.

### Status Bar

`.status-bar` class:
- Background: `var(--bg-panel-crt)`
- Border: `1px solid var(--border-crt)`
- Padding: `6px 10px`
- Font size: 11px

### Scanline Overlay

Updated from neutral black to green-tinted:
```css
background: repeating-linear-gradient(
  0deg,
  transparent,
  transparent 2px,
  rgba(0, 255, 65, 0.015) 2px,
  rgba(0, 255, 65, 0.015) 4px
);
```

## Component Specifications

### index.css (Global)

- Add new CRT CSS variables to `:root`
- Add `.badge`, `.badge-green`, `.badge-amber`, `.badge-red` classes
- Add `.btn-action`, `.btn-danger` classes
- Update `.btn-primary` to CRT style
- Add `.section-heading` class
- Add `.card-crt` class with `:hover` pseudo-class
- Add `.status-bar` class
- Update scanline overlay to green-tinted
- Update base `button` styles to CRT palette (background: `var(--bg-card-crt)`, border-color: `var(--border-crt)`)
- Update scrollbar thumb to green-tinted (`var(--border-crt)`)
- Update `html, body, #root` background to `var(--bg-crt)`

### SessionCard.tsx

**Structure change:** The outer `<div>` + inner `<Link>` structure is retained for navigation. The `<Link>` remains the clickable area wrapping the card content. The destroy button remains outside the `<Link>`, in the outer flex container.

**Layout within `<Link>`:**
- Title row: `display: flex; align-items: center; gap: 10px` (replaces `space-between`)
  - Title: `hostname: command` in green with text-shadow glow
  - Mode badge: `.badge.badge-amber` — `[pty]` or `[pipe]`
  - Status badge (conditional):
    - If `lazy && !process_running && !process_exited`: `.badge.badge-green` `[ready]`
    - If `process_exited`: `.badge.badge-red` `[exited]`
    - If `process_running && !process_exited && !lazy`: no status badge (same as current behavior — running is the default/implied state)
- Metadata row: `viewers: N` / `id: xxx` using `color: var(--text-dim)`

**Outer wrapper:** `.card-crt` class. Remove `onMouseEnter`/`onMouseLeave` JS handlers — hover is now CSS.

**Destroy button:** `.btn-danger` — text: `[destroy]`

### SessionList.tsx

- Heading: `.section-heading` — `// ACTIVE SESSIONS =============== [N]`
- Loading state: green text with text-shadow glow
- Error state: red text with text-shadow glow, same layout as loading
- Empty state: existing ASCII prompt with green glow treatment on the `<pre>`, code block uses `.card-crt` styling
- Session list container: `display: flex; flex-direction: column; gap: 6px`

### TerminalView.tsx

- Status bar container: `.status-bar` class
- Back link: color `#00aa33` (dim green)
- Command label: amber with text-shadow glow
- Connection status as badge:
  - Connected: `.badge.badge-green` `[connected]`
  - Disconnected / ended: `.badge.badge-red` `[disconnected]`
  - Connecting: `.badge.badge-amber` `[connecting...]`
  - Error: `.badge.badge-red` with error text
- Process exited: `.badge.badge-amber` `[exited (N)]` + `.btn-action` `[restart]`
- Terminal container: border-color `var(--border-crt)`
- xterm.js theme: update `background` from `#0a0a0a` to `#050808` to match `--bg-crt`. All other theme colors unchanged.
- Pipe mode indicator: `color: var(--text-dim)`, centered

### Layout.tsx (header/footer)

- Header: `background: var(--bg-panel-crt)`, `border-bottom-color: var(--border-crt)`
- Logo text: `>_ phosphor` (add underscore). `text-shadow: 0 0 12px rgba(0, 255, 65, 0.5)`
- User email: `color: #00aa33` (dim green)
- Logout button: `.btn-action` — text: `[logout]`
- Settings link: `color: #00aa33`, no underline
- Releases link: `color: #00aa33`, no underline
- Footer: `color: var(--text-dim)`, `border-top-color: var(--border-crt)`

### ProtectedRoute.tsx (login screen)

- ASCII art: `text-shadow: 0 0 12px rgba(0, 255, 65, 0.5)` (stronger glow)
- Heading text: change from "Sign in to view your terminal sessions" to `// AUTHENTICATION REQUIRED` styled with `.section-heading` or equivalent inline green + glow
- Provider buttons: handled by ProviderButtons component

### ProviderButtons.tsx

- All buttons use bracket labels in JSX: `[sign in with Microsoft]`, `[sign in with Google]`, etc.
- First provider: `className="btn-primary"`
- Other providers: `className="btn-action"`
- Layout: `display: flex; gap: 12px`

### SettingsPage.tsx

- Page heading: `// SETTINGS` with `.section-heading` styling
- Section heading: `// API KEYS` with similar styling
- Generate button: `.btn-primary` — text: `[generate API key]`
- API key display box: `background: var(--bg-card-crt)`, `border: 1px solid var(--border-crt)`, green text with glow
- Copy button: `.btn-primary` (keeping it prominent since it's a primary action for the displayed key)
- Warning text: amber with text-shadow glow
- Pre/code blocks: `background: var(--bg-card-crt)`, `border: 1px solid var(--border-crt)`

### Destroy Modal (in SessionCard.tsx)

- Overlay: `rgba(0, 0, 0, 0.85)`
- Modal container: `background: var(--bg-card-crt)`, `border: 1px solid var(--border-crt)`, `box-shadow: inset 0 0 20px rgba(0, 255, 65, 0.02)`
- Title: `// DESTROY SESSION` in red with `text-shadow: 0 0 6px rgba(255, 51, 51, 0.4)`
- Warning text: `color: var(--text)`
- `<code>` within warning: green with glow (unchanged)
- Cancel button: `.btn-action` — text: `[cancel]`
- Confirm button: `.btn-danger` — text: `[destroy session]`

## Implementation Approach

1. Update `index.css` with all new variables and classes first
2. Update each component to use the new classes, removing inline styles
3. Each component is an independent unit — can be done in any order after CSS is in place
