package relay

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHandleListSessions_Empty(t *testing.T) {
	store := NewMemorySessionStore()
	hub := NewHub(store, nil, "test", slog.Default())
	authSessions := NewMemoryAuthSessionStore(5 * time.Minute)
	t.Cleanup(authSessions.Stop)
	s := &Server{hub: hub, logger: slog.Default(), devMode: true, authSessions: authSessions}

	r := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	w := httptest.NewRecorder()

	s.HandleListSessions(w, r)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var result []sessionListItem
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("got %d sessions, want 0", len(result))
	}
}

func TestHandleListSessions_WithSessions(t *testing.T) {
	store := NewMemorySessionStore()
	hub := NewHub(store, nil, "test", slog.Default())
	authSessions := NewMemoryAuthSessionStore(5 * time.Minute)
	t.Cleanup(authSessions.Stop)
	s := &Server{hub: hub, logger: slog.Default(), devMode: true, authSessions: authSessions}

	ctx := context.Background()

	info1 := SessionInfo{
		ID:            "s1",
		OwnerProvider: "dev",
		OwnerSub:      "anonymous",
		Mode:          "pty",
		Cols:          80,
		Rows:          24,
		Command:       "bash",
	}
	info2 := SessionInfo{
		ID:            "s2",
		OwnerProvider: "dev",
		OwnerSub:      "anonymous",
		Mode:          "pipe",
		Cols:          120,
		Rows:          40,
		Command:       "zsh",
	}
	hub.Register(ctx, info1, nil)
	hub.Register(ctx, info2, nil)

	r := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	w := httptest.NewRecorder()

	s.HandleListSessions(w, r)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var result []sessionListItem
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("got %d sessions, want 2", len(result))
	}

	// Build a map for order-independent assertions.
	byID := make(map[string]sessionListItem)
	for _, info := range result {
		byID[info.ID] = info
	}

	item1, ok := byID["s1"]
	if !ok {
		t.Fatal("session s1 not found in response")
	}
	if item1.Mode != "pty" {
		t.Errorf("s1 Mode = %q, want %q", item1.Mode, "pty")
	}
	if item1.Cols != 80 {
		t.Errorf("s1 Cols = %d, want 80", item1.Cols)
	}
	if item1.Rows != 24 {
		t.Errorf("s1 Rows = %d, want 24", item1.Rows)
	}
	if item1.Command != "bash" {
		t.Errorf("s1 Command = %q, want %q", item1.Command, "bash")
	}
	if item1.Viewers != 0 {
		t.Errorf("s1 Viewers = %d, want 0", item1.Viewers)
	}

	item2, ok := byID["s2"]
	if !ok {
		t.Fatal("session s2 not found in response")
	}
	if item2.Mode != "pipe" {
		t.Errorf("s2 Mode = %q, want %q", item2.Mode, "pipe")
	}
	if item2.Cols != 120 {
		t.Errorf("s2 Cols = %d, want 120", item2.Cols)
	}
	if item2.Rows != 40 {
		t.Errorf("s2 Rows = %d, want 40", item2.Rows)
	}
	if item2.Command != "zsh" {
		t.Errorf("s2 Command = %q, want %q", item2.Command, "zsh")
	}
}

func TestHandleListSessions_Unauthorized(t *testing.T) {
	store := NewMemorySessionStore()
	hub := NewHub(store, nil, "test", slog.Default())
	s := &Server{hub: hub, logger: slog.Default(), devMode: false}

	r := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	w := httptest.NewRecorder()

	s.HandleListSessions(w, r)

	resp := w.Result()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestHandleListSessions_LazySessionFields(t *testing.T) {
	store := NewMemorySessionStore()
	hub := NewHub(store, nil, "test", slog.Default())
	authSessions := NewMemoryAuthSessionStore(5 * time.Minute)
	t.Cleanup(authSessions.Stop)
	s := &Server{hub: hub, logger: slog.Default(), devMode: true, authSessions: authSessions}

	ctx := context.Background()

	// Register a lazy session with DelegateFor (simulating daemon creating a session)
	info := SessionInfo{
		ID:            "lazy1",
		OwnerProvider: "delegated",
		OwnerSub:      "user@example.com",
		Mode:          "pty",
		Cols:          80,
		Rows:          24,
		Command:       "bash",
		Lazy:          true,
		ProcessRunning: false,
		DelegateFor:   "user@example.com",
	}
	hub.Register(ctx, info, nil)

	// Query as the delegated identity (microsoft:user@example.com in dev mode token format)
	r := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	r.Header.Set("Authorization", "Bearer delegated:user@example.com")
	w := httptest.NewRecorder()

	s.HandleListSessions(w, r)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var result []sessionListItem
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("got %d sessions, want 1", len(result))
	}

	item := result[0]
	if item.ID != "lazy1" {
		t.Errorf("ID = %q, want %q", item.ID, "lazy1")
	}
	if !item.Lazy {
		t.Error("Lazy = false, want true")
	}
	if item.ProcessRunning {
		t.Error("ProcessRunning = true, want false")
	}
}

func TestHandleListSessions_DelegatedVisibility(t *testing.T) {
	store := NewMemorySessionStore()
	hub := NewHub(store, nil, "test", slog.Default())
	authSessions := NewMemoryAuthSessionStore(5 * time.Minute)
	t.Cleanup(authSessions.Stop)
	s := &Server{hub: hub, logger: slog.Default(), devMode: true, authSessions: authSessions}

	ctx := context.Background()

	// Session owned directly by microsoft:alice@example.com
	directSession := SessionInfo{
		ID:            "direct1",
		OwnerProvider: "microsoft",
		OwnerSub:      "alice@example.com",
		Mode:          "pty",
		Cols:          80,
		Rows:          24,
		Command:       "bash",
	}
	hub.Register(ctx, directSession, nil)

	// Delegated session: owned by delegated:alice@example.com (daemon acts on alice's behalf)
	delegatedSession := SessionInfo{
		ID:            "delegated1",
		OwnerProvider: "delegated",
		OwnerSub:      "alice@example.com",
		Mode:          "pty",
		Cols:          80,
		Rows:          24,
		Command:       "zsh",
		Lazy:          true,
		DelegateFor:   "alice@example.com",
	}
	hub.Register(ctx, delegatedSession, nil)

	// Query as microsoft:alice@example.com — should see BOTH sessions
	r := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	r.Header.Set("Authorization", "Bearer microsoft:alice@example.com")
	w := httptest.NewRecorder()

	s.HandleListSessions(w, r)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var result []sessionListItem
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("got %d sessions, want 2", len(result))
	}

	byID := make(map[string]sessionListItem)
	for _, item := range result {
		byID[item.ID] = item
	}

	if _, ok := byID["direct1"]; !ok {
		t.Error("direct session direct1 not found in response")
	}
	if _, ok := byID["delegated1"]; !ok {
		t.Error("delegated session delegated1 not found in response")
	}
}

func TestHandleDestroySession_DelegatedOwnership(t *testing.T) {
	store := NewMemorySessionStore()
	hub := NewHub(store, nil, "test", slog.Default())
	authSessions := NewMemoryAuthSessionStore(5 * time.Minute)
	t.Cleanup(authSessions.Stop)
	s := &Server{hub: hub, logger: slog.Default(), devMode: true, authSessions: authSessions}

	ctx := context.Background()

	// Delegated session
	info := SessionInfo{
		ID:            "del-destroy",
		OwnerProvider: "delegated",
		OwnerSub:      "bob@example.com",
		Mode:          "pty",
		Cols:          80,
		Rows:          24,
		Command:       "bash",
		DelegateFor:   "bob@example.com",
	}
	hub.Register(ctx, info, nil)

	// Destroy as microsoft:bob@example.com — should succeed via delegated match
	r := httptest.NewRequest(http.MethodDelete, "/api/sessions/del-destroy", nil)
	r.Header.Set("Authorization", "Bearer microsoft:bob@example.com")
	r.SetPathValue("id", "del-destroy")
	w := httptest.NewRecorder()

	s.HandleDestroySession(w, r)

	resp := w.Result()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}

	// Verify session is actually gone
	_, ok, _ := hub.Get(ctx, "del-destroy")
	if ok {
		t.Error("session del-destroy should have been removed")
	}
}
