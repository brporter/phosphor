package relay

import (
	"context"
	"testing"
	"time"
)

func TestAuthSessionStore_CreateAndGet(t *testing.T) {
	store := NewMemoryAuthSessionStore(5 * time.Minute)
	defer store.Stop()
	ctx := context.Background()

	sess, err := store.Create(ctx, "google", "test-verifier", "cli")
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}
	if sess.ID == "" {
		t.Fatal("session ID is empty")
	}
	if sess.Provider != "google" {
		t.Errorf("provider = %q, want google", sess.Provider)
	}
	if sess.CodeVerifier != "test-verifier" {
		t.Errorf("code_verifier = %q, want test-verifier", sess.CodeVerifier)
	}

	got, ok, getErr := store.Get(ctx, sess.ID)
	if getErr != nil {
		t.Fatalf("Get error: %v", getErr)
	}
	if !ok {
		t.Fatal("session not found")
	}
	if got.ID != sess.ID {
		t.Errorf("got ID %q, want %q", got.ID, sess.ID)
	}
}

func TestAuthSessionStore_Complete(t *testing.T) {
	store := NewMemoryAuthSessionStore(5 * time.Minute)
	defer store.Stop()
	ctx := context.Background()

	sess, err := store.Create(ctx, "apple", "verifier", "cli")
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}
	store.Complete(ctx, sess.ID, "the-id-token")

	got, ok, getErr := store.Get(ctx, sess.ID)
	if getErr != nil {
		t.Fatalf("Get error: %v", getErr)
	}
	if !ok {
		t.Fatal("session not found after complete")
	}
	if got.IDToken != "the-id-token" {
		t.Errorf("id_token = %q, want the-id-token", got.IDToken)
	}
}

func TestAuthSessionStore_Consume(t *testing.T) {
	store := NewMemoryAuthSessionStore(5 * time.Minute)
	defer store.Stop()
	ctx := context.Background()

	sess, err := store.Create(ctx, "microsoft", "verifier", "cli")
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}
	store.Complete(ctx, sess.ID, "token-value")

	token, ok, consumeErr := store.Consume(ctx, sess.ID)
	if consumeErr != nil {
		t.Fatalf("Consume error: %v", consumeErr)
	}
	if !ok {
		t.Fatal("consume returned false")
	}
	if token != "token-value" {
		t.Errorf("token = %q, want token-value", token)
	}

	// Second consume should fail (single-use)
	_, ok, _ = store.Consume(ctx, sess.ID)
	if ok {
		t.Error("second consume should return false")
	}
}

func TestAuthSessionStore_Expiry(t *testing.T) {
	store := NewMemoryAuthSessionStore(50 * time.Millisecond)
	defer store.Stop()
	ctx := context.Background()

	sess, err := store.Create(ctx, "google", "verifier", "cli")
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	_, ok, _ := store.Get(ctx, sess.ID)
	if ok {
		t.Error("session should have expired")
	}
}
