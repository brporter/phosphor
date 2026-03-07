package relay

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBlocklist_EmptyPath(t *testing.T) {
	b := NewBlocklist("")
	defer b.Stop()

	if b.IsRevoked("anything") {
		t.Error("expected no keys revoked with empty path")
	}
}

func TestBlocklist_MissingFile(t *testing.T) {
	b := NewBlocklist("/tmp/blocklist_nonexistent_file_test.txt")
	defer b.Stop()

	if b.IsRevoked("anything") {
		t.Error("expected no keys revoked with missing file")
	}
}

func TestBlocklist_LoadFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "revoked.txt")

	content := "key1\n\n  \nkey2\nkey3\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("writing test file: %v", err)
	}

	b := NewBlocklist(path)
	defer b.Stop()

	for _, id := range []string{"key1", "key2", "key3"} {
		if !b.IsRevoked(id) {
			t.Errorf("expected %q to be revoked", id)
		}
	}

	if b.IsRevoked("key4") {
		t.Error("expected key4 to NOT be revoked")
	}
	if b.IsRevoked("") {
		t.Error("expected empty string to NOT be revoked")
	}
}

func TestBlocklist_FileWatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "revoked.txt")

	// Write initial file.
	if err := os.WriteFile(path, []byte("key1\n"), 0644); err != nil {
		t.Fatalf("writing test file: %v", err)
	}

	b := NewBlocklist(path)
	defer b.Stop()

	if !b.IsRevoked("key1") {
		t.Fatal("expected key1 to be revoked after initial load")
	}
	if b.IsRevoked("key2") {
		t.Fatal("expected key2 to NOT be revoked initially")
	}

	// Update the file with a new key.
	if err := os.WriteFile(path, []byte("key1\nkey2\n"), 0644); err != nil {
		t.Fatalf("updating test file: %v", err)
	}

	// Wait for the watcher to pick up the change.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if b.IsRevoked("key2") {
			return // success
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Error("expected key2 to be revoked after file update, but it was not")
}
