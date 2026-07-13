package store

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
)

// testStore connects to TEST_DATABASE_URL and cleans all tables, skipping the
// test when the env var is unset.
func testStore(t *testing.T) *Store {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	s, err := New(context.Background(), url)
	if err != nil {
		t.Fatalf("connecting to test database: %v", err)
	}
	t.Cleanup(s.Close)
	_, err = s.pool.Exec(context.Background(),
		`TRUNCATE tenants, users, machines, api_keys CASCADE`)
	if err != nil {
		t.Fatalf("truncating tables: %v", err)
	}
	return s
}

func TestGetOrCreateUser(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	u1, err := s.GetOrCreateUser(ctx, "google", "sub123", "alice@example.com")
	if err != nil {
		t.Fatalf("first GetOrCreateUser: %v", err)
	}
	if u1.TenantID == uuid.Nil {
		t.Error("expected tenant to be created")
	}
	if u1.Email != "alice@example.com" {
		t.Errorf("email = %q", u1.Email)
	}

	u2, err := s.GetOrCreateUser(ctx, "google", "sub123", "alice@example.com")
	if err != nil {
		t.Fatalf("second GetOrCreateUser: %v", err)
	}
	if u2.ID != u1.ID || u2.TenantID != u1.TenantID {
		t.Errorf("expected same user/tenant, got %v/%v vs %v/%v", u2.ID, u2.TenantID, u1.ID, u1.TenantID)
	}

	// Email update on re-login
	u3, err := s.GetOrCreateUser(ctx, "google", "sub123", "alice@new.example.com")
	if err != nil {
		t.Fatalf("third GetOrCreateUser: %v", err)
	}
	if u3.Email != "alice@new.example.com" {
		t.Errorf("email not updated: %q", u3.Email)
	}

	// Distinct identity gets a distinct tenant
	u4, err := s.GetOrCreateUser(ctx, "microsoft", "sub123", "bob@example.com")
	if err != nil {
		t.Fatalf("GetOrCreateUser for second identity: %v", err)
	}
	if u4.TenantID == u1.TenantID {
		t.Error("expected a fresh personal tenant for a new identity")
	}
}

func TestGetOrCreateUserConcurrent(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	const n = 8
	users := make(chan *User, n)
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			u, err := s.GetOrCreateUser(ctx, "google", "race-sub", "race@example.com")
			if err != nil {
				errs <- err
				return
			}
			users <- u
		}()
	}

	var first *User
	for i := 0; i < n; i++ {
		select {
		case err := <-errs:
			t.Fatalf("concurrent GetOrCreateUser: %v", err)
		case u := <-users:
			if first == nil {
				first = u
			} else if u.ID != first.ID {
				t.Errorf("got two different users: %v vs %v", u.ID, first.ID)
			}
		}
	}

	// Only one tenant should exist for the racing identity.
	var count int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM tenants`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("expected 1 tenant, found %d (orphan tenants leaked)", count)
	}
}

func TestMachineCRUD(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	u, err := s.GetOrCreateUser(ctx, "google", "sub1", "a@example.com")
	if err != nil {
		t.Fatal(err)
	}

	m, err := s.CreateMachine(ctx, u.TenantID, "laptop", "laptop.local", "SHA256:abc")
	if err != nil {
		t.Fatalf("CreateMachine: %v", err)
	}
	if m.LastSeenAt != nil {
		t.Error("new machine should have nil last_seen_at")
	}

	// Duplicate name within tenant
	if _, err := s.CreateMachine(ctx, u.TenantID, "laptop", "", "SHA256:other"); !errors.Is(err, ErrDuplicateName) {
		t.Errorf("duplicate name: got %v, want ErrDuplicateName", err)
	}
	// Duplicate fingerprint across tenants
	other, err := s.GetOrCreateUser(ctx, "google", "sub2", "b@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateMachine(ctx, other.TenantID, "desktop", "", "SHA256:abc"); !errors.Is(err, ErrDuplicateFingerprint) {
		t.Errorf("duplicate fingerprint: got %v, want ErrDuplicateFingerprint", err)
	}
	if _, err := s.CreateMachine(ctx, other.TenantID, "desktop", "", "SHA256:def"); err != nil {
		t.Fatalf("CreateMachine for other tenant: %v", err)
	}

	got, err := s.GetMachineByFingerprint(ctx, "SHA256:abc")
	if err != nil || got.ID != m.ID {
		t.Fatalf("GetMachineByFingerprint: %v, %v", got, err)
	}

	if err := s.TouchMachine(ctx, m.ID); err != nil {
		t.Fatalf("TouchMachine: %v", err)
	}
	got, err = s.GetMachine(ctx, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.LastSeenAt == nil {
		t.Error("last_seen_at not set after touch")
	}

	if err := s.RenameMachine(ctx, m.ID, "workbox"); err != nil {
		t.Fatalf("RenameMachine: %v", err)
	}

	list, err := s.ListMachines(ctx, u.TenantID)
	if err != nil || len(list) != 1 || list[0].Name != "workbox" {
		t.Fatalf("ListMachines: %v, %v", list, err)
	}
	// Other tenant sees nothing of ours
	list, err = s.ListMachines(ctx, other.TenantID)
	if err != nil || len(list) != 1 || list[0].Name != "desktop" {
		t.Fatalf("ListMachines(other): %v, %v", list, err)
	}

	if err := s.DeleteMachine(ctx, m.ID); err != nil {
		t.Fatalf("DeleteMachine: %v", err)
	}
	if _, err := s.GetMachine(ctx, m.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("after delete: got %v, want ErrNotFound", err)
	}
}

func TestAPIKeyRevocation(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	u, err := s.GetOrCreateUser(ctx, "google", "sub1", "a@example.com")
	if err != nil {
		t.Fatal(err)
	}

	// Unknown keys are revoked.
	revoked, err := s.IsAPIKeyRevoked(ctx, "unknown-key")
	if err != nil || !revoked {
		t.Errorf("unknown key: revoked=%v err=%v, want true", revoked, err)
	}

	if err := s.RecordAPIKey(ctx, "key1", u.ID); err != nil {
		t.Fatalf("RecordAPIKey: %v", err)
	}
	revoked, err = s.IsAPIKeyRevoked(ctx, "key1")
	if err != nil || revoked {
		t.Errorf("fresh key: revoked=%v err=%v, want false", revoked, err)
	}

	// Wrong user can't revoke.
	other, _ := s.GetOrCreateUser(ctx, "google", "sub2", "b@example.com")
	if err := s.RevokeAPIKey(ctx, "key1", other.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("cross-user revoke: got %v, want ErrNotFound", err)
	}

	if err := s.RevokeAPIKey(ctx, "key1", u.ID); err != nil {
		t.Fatalf("RevokeAPIKey: %v", err)
	}
	revoked, err = s.IsAPIKeyRevoked(ctx, "key1")
	if err != nil || !revoked {
		t.Errorf("revoked key: revoked=%v err=%v, want true", revoked, err)
	}

	// Double revoke is ErrNotFound (already revoked).
	if err := s.RevokeAPIKey(ctx, "key1", u.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("double revoke: got %v, want ErrNotFound", err)
	}
}

func TestMigrationsIdempotent(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	for i := 0; i < 2; i++ {
		if err := migrateUp(url); err != nil {
			t.Fatalf("migrateUp run %d: %v", i+1, err)
		}
	}
}
