# Dev Auth Provider Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a "dev" authentication provider that allows instant login when the relay runs with `DEV_MODE=1` and no real OIDC providers are configured.

**Architecture:** The relay exposes a `GET /api/auth/config` endpoint that returns the list of available providers (including "dev" when in dev mode). The SPA fetches this on load and renders buttons dynamically. When "dev" is selected, the relay short-circuits the OIDC flow — creating an auth session pre-completed with a synthetic JWT — and the SPA polls immediately instead of redirecting.

**Tech Stack:** Go (relay), React/TypeScript (SPA)

---

### Task 1: Relay — Add `HandleAuthConfig` endpoint

**Files:**
- Modify: `internal/relay/handler_auth.go`
- Modify: `internal/relay/server.go`
- Test: `internal/relay/handler_auth_test.go`

Adds `GET /api/auth/config` returning `{"providers":["dev"]}` (or real provider names). When `devMode=true`, "dev" is prepended to the list.

### Task 2: Relay — Handle dev provider in `HandleAuthLogin`

**Files:**
- Modify: `internal/relay/handler_auth.go`
- Test: `internal/relay/handler_auth_test.go`

When provider is "dev" and devMode is true: create session, generate synthetic JWT, immediately complete the session, return empty `auth_url`. When devMode is false, reject "dev" as unknown.

### Task 3: SPA — Fetch providers and expose via AuthContext

**Files:**
- Modify: `web/src/lib/api.ts`
- Modify: `web/src/auth/AuthProvider.tsx`

Add `fetchAuthConfig()` to api.ts. AuthProvider fetches providers on mount, exposes them in context. Login function skips redirect when `auth_url` is empty (polls immediately).

### Task 4: SPA — Dynamic provider buttons

**Files:**
- Modify: `web/src/components/ProtectedRoute.tsx`
- Modify: `web/src/components/Layout.tsx`

Replace hardcoded Microsoft/Google/Apple buttons with dynamic rendering based on providers from context.

### Task 5: Build, test, verify

Run Go tests, build SPA, confirm everything passes.
