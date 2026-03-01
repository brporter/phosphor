package relay

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"testing"
)

// newTestHub creates a Hub backed by in-memory store with no bus.
func newTestHub() *Hub {
	store := NewMemorySessionStore()
	h := NewHub(store, nil, "test", slog.Default())
	store.SetExpiryCallback(func(ctx context.Context, id string) {
		h.Unregister(ctx, id)
	})
	return h
}

// newTestSessionInfo creates a minimal SessionInfo for hub tests.
func newTestSessionInfo(id, provider, sub string) SessionInfo {
	return SessionInfo{
		ID:            id,
		OwnerProvider: provider,
		OwnerSub:      sub,
	}
}

func TestHub_RegisterAndGet(t *testing.T) {
	h := newTestHub()
	ctx := context.Background()
	info := newTestSessionInfo("sess-1", "google", "user-1")

	h.Register(ctx, info, nil)

	got, ok, err := h.Get(ctx, "sess-1")
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
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
	h := newTestHub()
	ctx := context.Background()

	_, ok, err := h.Get(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if ok {
		t.Error("Get returned true on empty hub, want false")
	}
}

func TestHub_Unregister(t *testing.T) {
	h := newTestHub()
	ctx := context.Background()
	info := newTestSessionInfo("sess-2", "microsoft", "user-2")

	h.Register(ctx, info, nil)
	h.Unregister(ctx, "sess-2")

	_, ok, _ := h.Get(ctx, "sess-2")
	if ok {
		t.Error("Get returned true after Unregister, want false")
	}
}

func TestHub_ListForOwner(t *testing.T) {
	h := newTestHub()
	ctx := context.Background()

	h.Register(ctx, newTestSessionInfo("alice-sess-1", "google", "alice"), nil)
	h.Register(ctx, newTestSessionInfo("alice-sess-2", "google", "alice"), nil)
	h.Register(ctx, newTestSessionInfo("bob-sess-1", "google", "bob"), nil)

	aliceSessions, err := h.ListForOwner(ctx, "google", "alice")
	if err != nil {
		t.Fatalf("ListForOwner error: %v", err)
	}
	if len(aliceSessions) != 2 {
		t.Errorf("ListForOwner(alice) returned %d sessions, want 2", len(aliceSessions))
	}

	bobSessions, _ := h.ListForOwner(ctx, "google", "bob")
	if len(bobSessions) != 1 {
		t.Errorf("ListForOwner(bob) returned %d sessions, want 1", len(bobSessions))
	}
	if bobSessions[0].ID != "bob-sess-1" {
		t.Errorf("bob's session ID = %q, want bob-sess-1", bobSessions[0].ID)
	}

	noSessions, _ := h.ListForOwner(ctx, "microsoft", "alice")
	if len(noSessions) != 0 {
		t.Errorf("ListForOwner(microsoft, alice) returned %d sessions, want 0", len(noSessions))
	}
}

func TestHub_CloseAll(t *testing.T) {
	h := newTestHub()
	ctx := context.Background()

	h.Register(ctx, newTestSessionInfo("s1", "google", "u1"), nil)
	h.Register(ctx, newTestSessionInfo("s2", "google", "u2"), nil)

	h.CloseAll()

	_, ok1, _ := h.Get(ctx, "s1")
	_, ok2, _ := h.Get(ctx, "s2")
	// CloseAll only clears locals; store entries remain.
	// But locals should be gone.
	_, lok1 := h.GetLocal("s1")
	_, lok2 := h.GetLocal("s2")
	if lok1 || lok2 {
		t.Error("local sessions still present after CloseAll")
	}
	// Store still has entries (CloseAll doesn't purge store for graceful shutdown).
	_ = ok1
	_ = ok2
}

func TestHub_ConcurrentAccess(t *testing.T) {
	h := newTestHub()
	ctx := context.Background()

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(n int) {
			defer wg.Done()
			id := fmt.Sprintf("concurrent-sess-%d", n)
			info := newTestSessionInfo(id, "google", fmt.Sprintf("user-%d", n))

			h.Register(ctx, info, nil)
			h.Get(ctx, id)
			h.ListForOwner(ctx, "google", fmt.Sprintf("user-%d", n))
			h.Unregister(ctx, id)
			h.Get(ctx, id)
		}(i)
	}

	wg.Wait()
}
