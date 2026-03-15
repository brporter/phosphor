package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"log/slog"

	"github.com/brporter/phosphor/internal/protocol"
)

func testReceiver(t *testing.T) (*FileReceiver, string) {
	t.Helper()
	dir := t.TempDir()
	logger := slog.Default()
	fr := NewFileReceiver(logger, dir)
	t.Cleanup(fr.Close)
	return fr, dir
}

func makeFileStartPayload(id, name string, size int64) []byte {
	data, _ := json.Marshal(protocol.FileStart{ID: id, Name: name, Size: size})
	return data
}

func makeFileEndPayload(id, hash string) []byte {
	data, _ := json.Marshal(protocol.FileEnd{ID: id, SHA256: hash})
	return data
}

func makeFileChunkPayload(id string, data []byte) []byte {
	payload := make([]byte, fileChunkIDLen+len(data))
	copy(payload[:fileChunkIDLen], id)
	copy(payload[fileChunkIDLen:], data)
	return payload
}

func TestHandleFileStart_ValidFile(t *testing.T) {
	fr, _ := testReceiver(t)

	payload := makeFileStartPayload("abcd1234", "test.txt", 100)
	ack, err := fr.HandleFileStart(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ack.Status != "accepted" {
		t.Errorf("status: got %q, want accepted", ack.Status)
	}
	if ack.ID != "abcd1234" {
		t.Errorf("id: got %q, want abcd1234", ack.ID)
	}
}

func TestHandleFileStart_PathTraversal(t *testing.T) {
	fr, _ := testReceiver(t)

	cases := []struct {
		name     string
		filename string
	}{
		{"dotdot", "../etc/passwd"},
		{"slash", "foo/bar"},
		{"backslash", "foo\\bar"},
		{"absolute_unix", "/etc/passwd"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := makeFileStartPayload("abcd1234", tc.filename, 100)
			ack, err := fr.HandleFileStart(payload)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ack.Status != "error" {
				t.Errorf("expected error status for filename %q, got %q", tc.filename, ack.Status)
			}
		})
	}
}

func TestHandleFileStart_DotFile(t *testing.T) {
	fr, _ := testReceiver(t)

	payload := makeFileStartPayload("abcd1234", ".hidden", 100)
	ack, err := fr.HandleFileStart(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ack.Status != "error" {
		t.Errorf("expected error status for dot file, got %q", ack.Status)
	}
}

func TestHandleFileStart_InvalidTransferID(t *testing.T) {
	fr, _ := testReceiver(t)

	cases := []struct {
		name string
		id   string
	}{
		{"too_short", "abc"},
		{"too_long", "abcdefghij"},
		{"special_chars", "abcd!@#$"},
		{"empty", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := makeFileStartPayload(tc.id, "test.txt", 100)
			ack, err := fr.HandleFileStart(payload)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ack.Status != "error" {
				t.Errorf("expected error status for ID %q, got %q", tc.id, ack.Status)
			}
		})
	}
}

func TestHandleFileStart_InvalidSize(t *testing.T) {
	fr, _ := testReceiver(t)

	cases := []struct {
		name string
		size int64
	}{
		{"zero", 0},
		{"negative", -1},
		{"too_large", maxFileSize + 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := makeFileStartPayload("abcd1234", "test.txt", tc.size)
			ack, err := fr.HandleFileStart(payload)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ack.Status != "error" {
				t.Errorf("expected error status for size %d, got %q", tc.size, ack.Status)
			}
		})
	}
}

func TestHandleFileStart_DuplicateID(t *testing.T) {
	fr, _ := testReceiver(t)

	// First transfer
	payload1 := makeFileStartPayload("abcd1234", "file1.txt", 100)
	ack1, err := fr.HandleFileStart(payload1)
	if err != nil || ack1.Status != "accepted" {
		t.Fatalf("first start failed: err=%v status=%s", err, ack1.Status)
	}

	// Second transfer with same ID replaces the first
	payload2 := makeFileStartPayload("abcd1234", "file2.txt", 200)
	ack2, err := fr.HandleFileStart(payload2)
	if err != nil {
		t.Fatalf("second start error: %v", err)
	}
	if ack2.Status != "accepted" {
		t.Errorf("second start status: got %q, want accepted", ack2.Status)
	}

	// Only one transfer should exist
	fr.mu.Lock()
	count := len(fr.transfers)
	fr.mu.Unlock()
	if count != 1 {
		t.Errorf("expected 1 transfer, got %d", count)
	}
}

func TestHandleFileChunk_WritesData(t *testing.T) {
	fr, dir := testReceiver(t)

	// Start transfer
	startPayload := makeFileStartPayload("abcd1234", "test.txt", 10)
	ack, err := fr.HandleFileStart(startPayload)
	if err != nil || ack.Status != "accepted" {
		t.Fatalf("start failed: err=%v status=%s", err, ack.Status)
	}

	// Send two chunks
	chunk1 := makeFileChunkPayload("abcd1234", []byte("hello"))
	ack1, err := fr.HandleFileChunk(chunk1)
	if err != nil {
		t.Fatalf("chunk1 error: %v", err)
	}
	if ack1.Status != "progress" {
		t.Errorf("chunk1 status: got %q, want progress", ack1.Status)
	}
	if ack1.BytesWritten != 5 {
		t.Errorf("chunk1 bytes_written: got %d, want 5", ack1.BytesWritten)
	}

	chunk2 := makeFileChunkPayload("abcd1234", []byte("world"))
	ack2, err := fr.HandleFileChunk(chunk2)
	if err != nil {
		t.Fatalf("chunk2 error: %v", err)
	}
	if ack2.BytesWritten != 10 {
		t.Errorf("chunk2 bytes_written: got %d, want 10", ack2.BytesWritten)
	}

	// Verify temp file content
	tmpPath := filepath.Join(dir, ".test.txt.phosphor-tmp")
	content, err := os.ReadFile(tmpPath)
	if err != nil {
		t.Fatalf("read temp file: %v", err)
	}
	if string(content) != "helloworld" {
		t.Errorf("temp file content: got %q, want helloworld", string(content))
	}
}

func TestHandleFileChunk_SizeExceeded(t *testing.T) {
	fr, dir := testReceiver(t)

	// Start transfer with size=5
	startPayload := makeFileStartPayload("abcd1234", "test.txt", 5)
	ack, err := fr.HandleFileStart(startPayload)
	if err != nil || ack.Status != "accepted" {
		t.Fatalf("start failed: err=%v status=%s", err, ack.Status)
	}

	// Send a chunk that exceeds declared size
	chunk := makeFileChunkPayload("abcd1234", []byte("this is way too long"))
	ack2, err := fr.HandleFileChunk(chunk)
	if err != nil {
		t.Fatalf("chunk error: %v", err)
	}
	if ack2.Status != "error" {
		t.Errorf("expected error status for oversized chunk, got %q", ack2.Status)
	}

	// Transfer should be cleaned up
	fr.mu.Lock()
	_, exists := fr.transfers["abcd1234"]
	fr.mu.Unlock()
	if exists {
		t.Error("transfer should be cleaned up after size exceeded")
	}

	// Temp file should be removed
	tmpPath := filepath.Join(dir, ".test.txt.phosphor-tmp")
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Error("temp file should be removed after size exceeded")
	}
}

func TestHandleFileChunk_UnknownTransfer(t *testing.T) {
	fr, _ := testReceiver(t)

	chunk := makeFileChunkPayload("unknown1", []byte("data"))
	ack, err := fr.HandleFileChunk(chunk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ack.Status != "error" {
		t.Errorf("expected error status, got %q", ack.Status)
	}
}

func TestHandleFileEnd_HashMatch(t *testing.T) {
	fr, dir := testReceiver(t)

	data := []byte("hello world")
	h := sha256.Sum256(data)
	hash := hex.EncodeToString(h[:])

	// Start
	startPayload := makeFileStartPayload("abcd1234", "test.txt", int64(len(data)))
	fr.HandleFileStart(startPayload)

	// Chunk
	chunk := makeFileChunkPayload("abcd1234", data)
	fr.HandleFileChunk(chunk)

	// End
	endPayload := makeFileEndPayload("abcd1234", hash)
	ack, err := fr.HandleFileEnd(endPayload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ack.Status != "complete" {
		t.Errorf("status: got %q, want complete", ack.Status)
	}

	// Verify final file exists
	finalPath := filepath.Join(dir, "test.txt")
	content, err := os.ReadFile(finalPath)
	if err != nil {
		t.Fatalf("read final file: %v", err)
	}
	if string(content) != "hello world" {
		t.Errorf("final file content: got %q, want %q", string(content), "hello world")
	}

	// Verify temp file is removed
	tmpPath := filepath.Join(dir, ".test.txt.phosphor-tmp")
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Error("temp file should be removed after successful transfer")
	}
}

func TestHandleFileEnd_HashMismatch(t *testing.T) {
	fr, dir := testReceiver(t)

	// Start
	startPayload := makeFileStartPayload("abcd1234", "test.txt", 5)
	fr.HandleFileStart(startPayload)

	// Chunk
	chunk := makeFileChunkPayload("abcd1234", []byte("hello"))
	fr.HandleFileChunk(chunk)

	// End with wrong hash
	endPayload := makeFileEndPayload("abcd1234", "0000000000000000000000000000000000000000000000000000000000000000")
	ack, err := fr.HandleFileEnd(endPayload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ack.Status != "error" {
		t.Errorf("status: got %q, want error", ack.Status)
	}

	// Verify temp file is cleaned up
	tmpPath := filepath.Join(dir, ".test.txt.phosphor-tmp")
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Error("temp file should be removed after hash mismatch")
	}
}

func TestHandleFileEnd_SizeMismatch(t *testing.T) {
	fr, _ := testReceiver(t)

	data := []byte("hi")
	h := sha256.Sum256(data)
	hash := hex.EncodeToString(h[:])

	// Start with declared size=10 but only send 2 bytes
	startPayload := makeFileStartPayload("abcd1234", "test.txt", 10)
	fr.HandleFileStart(startPayload)

	chunk := makeFileChunkPayload("abcd1234", data)
	fr.HandleFileChunk(chunk)

	endPayload := makeFileEndPayload("abcd1234", hash)
	ack, err := fr.HandleFileEnd(endPayload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ack.Status != "error" {
		t.Errorf("status: got %q, want error (size mismatch)", ack.Status)
	}
}

func TestHandleFileEnd_OverwriteProtection(t *testing.T) {
	fr, dir := testReceiver(t)

	// Create the target file first
	targetPath := filepath.Join(dir, "existing.txt")
	os.WriteFile(targetPath, []byte("existing"), 0o644)

	// Start
	startPayload := makeFileStartPayload("abcd1234", "existing.txt", 5)
	ack, err := fr.HandleFileStart(startPayload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ack.Status != "error" {
		t.Errorf("expected error for existing file, got %q", ack.Status)
	}
}

func TestStaleTransferCleanup(t *testing.T) {
	fr, dir := testReceiver(t)

	// Start a transfer
	startPayload := makeFileStartPayload("stale123", "stale.txt", 100)
	fr.HandleFileStart(startPayload)

	// Manually set the lastAct to be old
	fr.mu.Lock()
	if transfer, ok := fr.transfers["stale123"]; ok {
		transfer.lastAct = time.Now().Add(-2 * staleTransferTimeout)
	}
	fr.mu.Unlock()

	// Start another transfer to trigger cleanup
	startPayload2 := makeFileStartPayload("new12345", "new.txt", 100)
	fr.HandleFileStart(startPayload2)

	// Check that stale transfer was cleaned up
	fr.mu.Lock()
	_, staleExists := fr.transfers["stale123"]
	_, newExists := fr.transfers["new12345"]
	fr.mu.Unlock()

	if staleExists {
		t.Error("stale transfer should have been cleaned up")
	}
	if !newExists {
		t.Error("new transfer should exist")
	}

	// Verify stale temp file was removed
	staleTmp := filepath.Join(dir, ".stale.txt.phosphor-tmp")
	if _, err := os.Stat(staleTmp); !os.IsNotExist(err) {
		t.Error("stale temp file should be removed")
	}
}

func TestFileReceiverRejectsSymlinkTmpPath(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	fr := NewFileReceiver(logger, dir)
	defer fr.Close()

	// Create a symlink at the temp path location
	targetFile := filepath.Join(dir, "evil-target")
	os.WriteFile(targetFile, []byte("original"), 0o644)
	symlinkPath := filepath.Join(dir, ".testfile.txt.phosphor-tmp")
	if err := os.Symlink(targetFile, symlinkPath); err != nil {
		t.Skip("symlinks not supported:", err)
	}

	// FileStart should fail because the tmp path already exists (as a symlink)
	payload, _ := json.Marshal(protocol.FileStart{
		ID:   "abcd1234",
		Name: "testfile.txt",
		Size: 5,
	})
	ack, err := fr.HandleFileStart(payload)
	if err != nil {
		t.Fatal(err)
	}
	if ack.Status != "error" {
		t.Errorf("expected error status, got %s", ack.Status)
	}

	// Verify the original file was not modified
	content, _ := os.ReadFile(targetFile)
	if string(content) != "original" {
		t.Error("symlink target was modified")
	}
}

func TestValidateTransferID(t *testing.T) {
	valid := []string{"abcd1234", "ABCD1234", "aB3dE6gH"}
	for _, id := range valid {
		if err := validateTransferID(id); err != nil {
			t.Errorf("expected valid ID %q, got error: %v", id, err)
		}
	}

	invalid := []string{"", "short", "toolongid!", "abcd!@#$", "abcd 234"}
	for _, id := range invalid {
		if err := validateTransferID(id); err == nil {
			t.Errorf("expected error for ID %q", id)
		}
	}
}
