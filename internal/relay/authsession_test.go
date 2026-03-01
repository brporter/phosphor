package relay

import (
	"testing"
	"time"
)

func TestAuthSessionStore_CreateAndGet(t *testing.T) {
	store := NewAuthSessionStore(5 * time.Minute)
	defer store.Stop()

	sess := store.Create("google", "test-verifier")
	if sess.ID == "" {
		t.Fatal("session ID is empty")
	}
	if sess.Provider != "google" {
		t.Errorf("provider = %q, want google", sess.Provider)
	}
	if sess.CodeVerifier != "test-verifier" {
		t.Errorf("code_verifier = %q, want test-verifier", sess.CodeVerifier)
	}

	got, ok := store.Get(sess.ID)
	if !ok {
		t.Fatal("session not found")
	}
	if got.ID != sess.ID {
		t.Errorf("got ID %q, want %q", got.ID, sess.ID)
	}
}

func TestAuthSessionStore_Complete(t *testing.T) {
	store := NewAuthSessionStore(5 * time.Minute)
	defer store.Stop()

	sess := store.Create("apple", "verifier")
	store.Complete(sess.ID, "the-id-token")

	got, ok := store.Get(sess.ID)
	if !ok {
		t.Fatal("session not found after complete")
	}
	if got.IDToken != "the-id-token" {
		t.Errorf("id_token = %q, want the-id-token", got.IDToken)
	}
}

func TestAuthSessionStore_Consume(t *testing.T) {
	store := NewAuthSessionStore(5 * time.Minute)
	defer store.Stop()

	sess := store.Create("microsoft", "verifier")
	store.Complete(sess.ID, "token-value")

	token, ok := store.Consume(sess.ID)
	if !ok {
		t.Fatal("consume returned false")
	}
	if token != "token-value" {
		t.Errorf("token = %q, want token-value", token)
	}

	// Second consume should fail (single-use)
	_, ok = store.Consume(sess.ID)
	if ok {
		t.Error("second consume should return false")
	}
}

func TestAuthSessionStore_Expiry(t *testing.T) {
	store := NewAuthSessionStore(50 * time.Millisecond)
	defer store.Stop()

	sess := store.Create("google", "verifier")
	time.Sleep(100 * time.Millisecond)

	_, ok := store.Get(sess.ID)
	if ok {
		t.Error("session should have expired")
	}
}
