package relay

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/coder/websocket"
)

func TestHandleListSessions_Empty(t *testing.T) {
	s := &Server{hub: NewHub(slog.Default()), logger: slog.Default(), devMode: true}

	r := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	w := httptest.NewRecorder()

	s.HandleListSessions(w, r)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var result []sessionInfo
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("got %d sessions, want 0", len(result))
	}
}

func TestHandleListSessions_WithSessions(t *testing.T) {
	hub := NewHub(slog.Default())
	s := &Server{hub: hub, logger: slog.Default(), devMode: true}

	sess1 := &Session{
		ID:            "s1",
		OwnerProvider: "dev",
		OwnerSub:      "anonymous",
		Mode:          "pty",
		Cols:          80,
		Rows:          24,
		Command:       "bash",
		viewers:       make(map[string]*websocket.Conn),
		logger:        slog.Default(),
	}
	sess2 := &Session{
		ID:            "s2",
		OwnerProvider: "dev",
		OwnerSub:      "anonymous",
		Mode:          "pipe",
		Cols:          120,
		Rows:          40,
		Command:       "zsh",
		viewers:       make(map[string]*websocket.Conn),
		logger:        slog.Default(),
	}
	hub.Register(sess1)
	hub.Register(sess2)

	r := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	w := httptest.NewRecorder()

	s.HandleListSessions(w, r)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var result []sessionInfo
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("got %d sessions, want 2", len(result))
	}

	// Build a map for order-independent assertions.
	byID := make(map[string]sessionInfo)
	for _, info := range result {
		byID[info.ID] = info
	}

	info1, ok := byID["s1"]
	if !ok {
		t.Fatal("session s1 not found in response")
	}
	if info1.Mode != "pty" {
		t.Errorf("s1 Mode = %q, want %q", info1.Mode, "pty")
	}
	if info1.Cols != 80 {
		t.Errorf("s1 Cols = %d, want 80", info1.Cols)
	}
	if info1.Rows != 24 {
		t.Errorf("s1 Rows = %d, want 24", info1.Rows)
	}
	if info1.Command != "bash" {
		t.Errorf("s1 Command = %q, want %q", info1.Command, "bash")
	}
	if info1.Viewers != 0 {
		t.Errorf("s1 Viewers = %d, want 0", info1.Viewers)
	}

	info2, ok := byID["s2"]
	if !ok {
		t.Fatal("session s2 not found in response")
	}
	if info2.Mode != "pipe" {
		t.Errorf("s2 Mode = %q, want %q", info2.Mode, "pipe")
	}
	if info2.Cols != 120 {
		t.Errorf("s2 Cols = %d, want 120", info2.Cols)
	}
	if info2.Rows != 40 {
		t.Errorf("s2 Rows = %d, want 40", info2.Rows)
	}
	if info2.Command != "zsh" {
		t.Errorf("s2 Command = %q, want %q", info2.Command, "zsh")
	}
}

func TestHandleListSessions_Unauthorized(t *testing.T) {
	s := &Server{hub: NewHub(slog.Default()), logger: slog.Default(), devMode: false}

	r := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	w := httptest.NewRecorder()

	s.HandleListSessions(w, r)

	resp := w.Result()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}
