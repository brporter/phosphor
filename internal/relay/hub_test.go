package relay

import (
	"fmt"
	"log/slog"
	"sync"
	"testing"

	"github.com/coder/websocket"
)

// newTestSession creates a minimal Session suitable for hub tests.
// cliConn is nil; Close() only iterates viewers so this is safe.
func newTestSession(id, provider, sub string) *Session {
	return &Session{
		ID:            id,
		OwnerProvider: provider,
		OwnerSub:      sub,
		viewers:       make(map[string]*websocket.Conn),
		logger:        slog.Default(),
	}
}

func TestHub_RegisterAndGet(t *testing.T) {
	h := NewHub(slog.Default())
	s := newTestSession("sess-1", "google", "user-1")

	h.Register(s)

	got, ok := h.Get("sess-1")
	if !ok {
		t.Fatal("Get returned false after Register")
	}
	if got.ID != "sess-1" {
		t.Errorf("got ID %q, want %q", got.ID, "sess-1")
	}
	if got.OwnerSub != "user-1" {
		t.Errorf("got OwnerSub %q, want %q", got.OwnerSub, "user-1")
	}
}

func TestHub_Get_NotFound(t *testing.T) {
	h := NewHub(slog.Default())

	_, ok := h.Get("nonexistent")
	if ok {
		t.Error("Get returned true on empty hub, want false")
	}
}

func TestHub_Unregister(t *testing.T) {
	h := NewHub(slog.Default())
	s := newTestSession("sess-2", "microsoft", "user-2")

	h.Register(s)
	h.Unregister("sess-2")

	_, ok := h.Get("sess-2")
	if ok {
		t.Error("Get returned true after Unregister, want false")
	}
}

func TestHub_ListForOwner(t *testing.T) {
	h := NewHub(slog.Default())

	// Two sessions owned by alice, one by bob
	alice1 := newTestSession("alice-sess-1", "google", "alice")
	alice2 := newTestSession("alice-sess-2", "google", "alice")
	bob1 := newTestSession("bob-sess-1", "google", "bob")

	h.Register(alice1)
	h.Register(alice2)
	h.Register(bob1)

	aliceSessions := h.ListForOwner("google", "alice")
	if len(aliceSessions) != 2 {
		t.Errorf("ListForOwner(alice) returned %d sessions, want 2", len(aliceSessions))
	}

	bobSessions := h.ListForOwner("google", "bob")
	if len(bobSessions) != 1 {
		t.Errorf("ListForOwner(bob) returned %d sessions, want 1", len(bobSessions))
	}
	if bobSessions[0].ID != "bob-sess-1" {
		t.Errorf("bob's session ID = %q, want bob-sess-1", bobSessions[0].ID)
	}

	// Different provider — same sub should not match
	noSessions := h.ListForOwner("microsoft", "alice")
	if len(noSessions) != 0 {
		t.Errorf("ListForOwner(microsoft, alice) returned %d sessions, want 0", len(noSessions))
	}
}

func TestHub_CloseAll(t *testing.T) {
	h := NewHub(slog.Default())

	h.Register(newTestSession("s1", "google", "u1"))
	h.Register(newTestSession("s2", "google", "u2"))

	h.CloseAll()

	_, ok1 := h.Get("s1")
	_, ok2 := h.Get("s2")
	if ok1 || ok2 {
		t.Error("sessions still present after CloseAll")
	}
}

func TestHub_ConcurrentAccess(t *testing.T) {
	h := NewHub(slog.Default())

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(n int) {
			defer wg.Done()
			id := fmt.Sprintf("concurrent-sess-%d", n)
			s := newTestSession(id, "google", fmt.Sprintf("user-%d", n))

			h.Register(s)
			h.Get(id)
			h.ListForOwner("google", fmt.Sprintf("user-%d", n))
			h.Unregister(id)
			h.Get(id)
		}(i)
	}

	wg.Wait()
}
