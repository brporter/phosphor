package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

func TestHandleFileChunk_UnknownTransfer(t *testing.T) {
	fr, _ := testReceiver(t)

	chunk := makeFileChunkPayload("unknown!", []byte("data"))
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
