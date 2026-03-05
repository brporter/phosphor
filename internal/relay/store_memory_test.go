package relay

import (
	"context"
	"testing"
	"time"
)

func TestMemorySessionStore_RegisterAndGet(t *testing.T) {
	store := NewMemorySessionStore()
	ctx := context.Background()

	info := SessionInfo{ID: "s1", OwnerProvider: "google", OwnerSub: "user-1", Mode: "pty", Cols: 80, Rows: 24}
	if err := store.Register(ctx, info); err != nil {
		t.Fatalf("Register error: %v", err)
	}

	got, ok, err := store.Get(ctx, "s1")
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if !ok {
		t.Fatal("Get returned false")
	}
	if got.ID != "s1" {
		t.Errorf("ID = %q, want s1", got.ID)
	}
	if got.OwnerSub != "user-1" {
		t.Errorf("OwnerSub = %q, want user-1", got.OwnerSub)
	}
	if got.Mode != "pty" {
		t.Errorf("Mode = %q, want pty", got.Mode)
	}
}

func TestMemorySessionStore_Get_NotFound(t *testing.T) {
	store := NewMemorySessionStore()
	ctx := context.Background()

	_, ok, err := store.Get(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if ok {
		t.Error("Get returned true for nonexistent session")
	}
}

func TestMemorySessionStore_Unregister(t *testing.T) {
	store := NewMemorySessionStore()
	ctx := context.Background()

	info := SessionInfo{ID: "s1", OwnerProvider: "google", OwnerSub: "u1"}
	store.Register(ctx, info)
	store.Unregister(ctx, "s1")

	_, ok, _ := store.Get(ctx, "s1")
	if ok {
		t.Error("session still found after Unregister")
	}
}

func TestMemorySessionStore_ListForOwner(t *testing.T) {
	store := NewMemorySessionStore()
	ctx := context.Background()

	store.Register(ctx, SessionInfo{ID: "a1", OwnerProvider: "google", OwnerSub: "alice"})
	store.Register(ctx, SessionInfo{ID: "a2", OwnerProvider: "google", OwnerSub: "alice"})
	store.Register(ctx, SessionInfo{ID: "b1", OwnerProvider: "google", OwnerSub: "bob"})

	aliceSessions, err := store.ListForOwner(ctx, "google", "alice")
	if err != nil {
		t.Fatalf("ListForOwner error: %v", err)
	}
	if len(aliceSessions) != 2 {
		t.Errorf("ListForOwner(alice) returned %d sessions, want 2", len(aliceSessions))
	}

	bobSessions, _ := store.ListForOwner(ctx, "google", "bob")
	if len(bobSessions) != 1 {
		t.Errorf("ListForOwner(bob) returned %d sessions, want 1", len(bobSessions))
	}
}

func TestMemorySessionStore_SetDisconnected(t *testing.T) {
	store := NewMemorySessionStore()
	ctx := context.Background()

	store.Register(ctx, SessionInfo{ID: "s1", OwnerProvider: "google", OwnerSub: "u1"})
	store.SetDisconnected(ctx, "s1")

	got, ok, _ := store.Get(ctx, "s1")
	if !ok {
		t.Fatal("session not found")
	}
	if !got.Disconnected {
		t.Error("Disconnected = false, want true")
	}
	if got.DisconnectedAt.IsZero() {
		t.Error("DisconnectedAt is zero")
	}
}

func TestMemorySessionStore_SetReconnected(t *testing.T) {
	store := NewMemorySessionStore()
	ctx := context.Background()

	store.Register(ctx, SessionInfo{ID: "s1", OwnerProvider: "google", OwnerSub: "u1"})
	store.SetDisconnected(ctx, "s1")
	store.SetReconnected(ctx, "s1", "new-token", "relay-2")

	got, ok, _ := store.Get(ctx, "s1")
	if !ok {
		t.Fatal("session not found")
	}
	if got.Disconnected {
		t.Error("Disconnected = true after SetReconnected")
	}
	if got.ReconnectToken != "new-token" {
		t.Errorf("ReconnectToken = %q, want new-token", got.ReconnectToken)
	}
	if got.RelayID != "relay-2" {
		t.Errorf("RelayID = %q, want relay-2", got.RelayID)
	}
}

func TestMemorySessionStore_UpdateDimensions(t *testing.T) {
	store := NewMemorySessionStore()
	ctx := context.Background()

	store.Register(ctx, SessionInfo{ID: "s1", OwnerProvider: "google", OwnerSub: "u1", Cols: 80, Rows: 24})
	store.UpdateDimensions(ctx, "s1", 120, 40)

	got, ok, _ := store.Get(ctx, "s1")
	if !ok {
		t.Fatal("session not found")
	}
	if got.Cols != 120 {
		t.Errorf("Cols = %d, want 120", got.Cols)
	}
	if got.Rows != 40 {
		t.Errorf("Rows = %d, want 40", got.Rows)
	}
}

func TestMemorySessionStore_ScheduleAndCancelExpiry(t *testing.T) {
	store := NewMemorySessionStore()
	ctx := context.Background()

	expired := make(chan string, 1)
	store.SetExpiryCallback(func(_ context.Context, id string) {
		expired <- id
	})

	store.Register(ctx, SessionInfo{ID: "s1", OwnerProvider: "google", OwnerSub: "u1"})
	store.ScheduleExpiry(ctx, "s1", 200*time.Millisecond)

	// Cancel before expiry
	store.CancelExpiry(ctx, "s1")

	// Wait past the original expiry time
	time.Sleep(400 * time.Millisecond)

	select {
	case <-expired:
		t.Error("expiry callback fired after CancelExpiry")
	default:
		// expected — callback should not have fired
	}
}

func TestMemorySessionStore_ExpiryCallbackFires(t *testing.T) {
	store := NewMemorySessionStore()
	ctx := context.Background()

	expired := make(chan string, 1)
	store.SetExpiryCallback(func(_ context.Context, id string) {
		expired <- id
	})

	store.Register(ctx, SessionInfo{ID: "s1", OwnerProvider: "google", OwnerSub: "u1"})
	store.ScheduleExpiry(ctx, "s1", 100*time.Millisecond)

	select {
	case id := <-expired:
		if id != "s1" {
			t.Errorf("expired id = %q, want s1", id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expiry callback did not fire within timeout")
	}
}

func TestMemoryStore_SetProcessRunning(t *testing.T) {
	store := NewMemorySessionStore()
	ctx := context.Background()

	store.Register(ctx, SessionInfo{
		ID:             "s1",
		OwnerProvider:  "google",
		OwnerSub:       "u1",
		Lazy:           true,
		ProcessRunning: false,
	})

	// Set ProcessRunning to true
	if err := store.SetProcessRunning(ctx, "s1", true); err != nil {
		t.Fatalf("SetProcessRunning(true) error: %v", err)
	}

	got, ok, err := store.Get(ctx, "s1")
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if !ok {
		t.Fatal("session not found")
	}
	if !got.ProcessRunning {
		t.Error("ProcessRunning = false, want true")
	}

	// Set ProcessRunning back to false
	if err := store.SetProcessRunning(ctx, "s1", false); err != nil {
		t.Fatalf("SetProcessRunning(false) error: %v", err)
	}

	got, ok, err = store.Get(ctx, "s1")
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if !ok {
		t.Fatal("session not found")
	}
	if got.ProcessRunning {
		t.Error("ProcessRunning = true, want false")
	}
}

func TestMemoryStore_LazyDelegateFields(t *testing.T) {
	store := NewMemorySessionStore()
	ctx := context.Background()

	store.Register(ctx, SessionInfo{
		ID:              "s1",
		OwnerProvider:   "google",
		OwnerSub:        "u1",
		Lazy:            true,
		DelegateFor:     "user@example.com",
		ServiceIdentity: "svc:daemon",
	})

	got, ok, err := store.Get(ctx, "s1")
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if !ok {
		t.Fatal("session not found")
	}
	if !got.Lazy {
		t.Error("Lazy = false, want true")
	}
	if got.DelegateFor != "user@example.com" {
		t.Errorf("DelegateFor = %q, want user@example.com", got.DelegateFor)
	}
	if got.ServiceIdentity != "svc:daemon" {
		t.Errorf("ServiceIdentity = %q, want svc:daemon", got.ServiceIdentity)
	}
}
