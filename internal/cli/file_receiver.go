package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/brporter/phosphor/internal/protocol"
)

const (
	fileChunkIDLen       = 8
	staleTransferTimeout = 60 * time.Second
	maxFileSize          = 100 << 20 // 100 MB
)

// FileReceiver handles incoming file transfers from viewers.
type FileReceiver struct {
	transfers map[string]*activeTransfer
	mu        sync.Mutex
	logger    *slog.Logger
	destDir   string // destination directory for received files
}

type activeTransfer struct {
	file      *os.File
	tmpPath   string
	finalPath string
	size      int64
	written   int64
	hash      hash.Hash
	lastAct   time.Time
}

// NewFileReceiver creates a FileReceiver that writes files to destDir.
func NewFileReceiver(logger *slog.Logger, destDir string) *FileReceiver {
	return &FileReceiver{
		transfers: make(map[string]*activeTransfer),
		logger:    logger,
		destDir:   destDir,
	}
}

// HandleFileStart processes a FileStart message and returns a FileAck.
func (fr *FileReceiver) HandleFileStart(payload []byte) (protocol.FileAck, error) {
	var fs protocol.FileStart
	if err := protocol.DecodeJSON(payload, &fs); err != nil {
		return protocol.FileAck{}, fmt.Errorf("decode FileStart: %w", err)
	}

	// Validate transfer ID: must be exactly 8 bytes, alphanumeric
	if err := validateTransferID(fs.ID); err != nil {
		return protocol.FileAck{
			ID:     fs.ID,
			Status: "error",
			Error:  err.Error(),
		}, nil
	}

	// Validate filename
	if err := validateFilename(fs.Name); err != nil {
		return protocol.FileAck{
			ID:     fs.ID,
			Status: "error",
			Error:  err.Error(),
		}, nil
	}

	// Validate file size
	if fs.Size <= 0 || fs.Size > maxFileSize {
		return protocol.FileAck{
			ID:     fs.ID,
			Status: "error",
			Error:  fmt.Sprintf("invalid file size: %d (max %d bytes)", fs.Size, maxFileSize),
		}, nil
	}

	fr.mu.Lock()
	defer fr.mu.Unlock()

	// Clean up stale transfers
	fr.cleanStaleLocked()

	// Reject duplicate transfer IDs
	if existing, ok := fr.transfers[fs.ID]; ok {
		existing.file.Close()
		os.Remove(existing.tmpPath)
		delete(fr.transfers, fs.ID)
		fr.logger.Warn("replaced duplicate transfer ID", "id", fs.ID)
	}

	finalPath := filepath.Join(fr.destDir, fs.Name)

	// Check if target already exists
	if _, err := os.Stat(finalPath); err == nil {
		return protocol.FileAck{
			ID:     fs.ID,
			Status: "error",
			Error:  "file already exists: " + fs.Name,
		}, nil
	}

	tmpPath := filepath.Join(fr.destDir, "."+fs.Name+".phosphor-tmp")
	f, err := os.Create(tmpPath)
	if err != nil {
		return protocol.FileAck{
			ID:     fs.ID,
			Status: "error",
			Error:  "failed to create temp file: " + err.Error(),
		}, nil
	}

	fr.transfers[fs.ID] = &activeTransfer{
		file:      f,
		tmpPath:   tmpPath,
		finalPath: finalPath,
		size:      fs.Size,
		hash:      sha256.New(),
		lastAct:   time.Now(),
	}

	fr.logger.Info("file transfer started", "id", fs.ID, "name", fs.Name, "size", fs.Size)

	return protocol.FileAck{
		ID:     fs.ID,
		Status: "accepted",
	}, nil
}

// HandleFileChunk processes a FileChunk binary payload and returns a FileAck.
// Payload format: [8-byte ASCII transfer ID][raw chunk data]
func (fr *FileReceiver) HandleFileChunk(payload []byte) (protocol.FileAck, error) {
	if len(payload) < fileChunkIDLen {
		return protocol.FileAck{}, fmt.Errorf("FileChunk payload too short")
	}

	id := string(payload[:fileChunkIDLen])
	data := payload[fileChunkIDLen:]

	fr.mu.Lock()
	t, ok := fr.transfers[id]
	if !ok {
		fr.mu.Unlock()
		return protocol.FileAck{
			ID:     id,
			Status: "error",
			Error:  "unknown transfer",
		}, nil
	}

	// Check size limit before writing
	if t.written+int64(len(data)) > t.size {
		// Overflow — clean up under lock
		t.file.Close()
		os.Remove(t.tmpPath)
		delete(fr.transfers, id)
		fr.mu.Unlock()
		return protocol.FileAck{
			ID:     id,
			Status: "error",
			Error:  fmt.Sprintf("size exceeded: declared %d bytes, received %d", t.size, t.written+int64(len(data))),
		}, nil
	}

	n, err := t.file.Write(data)
	if err != nil {
		t.file.Close()
		os.Remove(t.tmpPath)
		delete(fr.transfers, id)
		fr.mu.Unlock()
		return protocol.FileAck{
			ID:     id,
			Status: "error",
			Error:  "write failed: " + err.Error(),
		}, nil
	}

	t.hash.Write(data)
	t.written += int64(n)
	t.lastAct = time.Now()
	written := t.written
	fr.mu.Unlock()

	return protocol.FileAck{
		ID:           id,
		Status:       "progress",
		BytesWritten: written,
	}, nil
}

// HandleFileEnd processes a FileEnd message, verifies the hash, and finalizes the file.
func (fr *FileReceiver) HandleFileEnd(payload []byte) (protocol.FileAck, error) {
	var fe protocol.FileEnd
	if err := protocol.DecodeJSON(payload, &fe); err != nil {
		return protocol.FileAck{}, fmt.Errorf("decode FileEnd: %w", err)
	}

	fr.mu.Lock()
	t, ok := fr.transfers[fe.ID]
	if !ok {
		fr.mu.Unlock()
		return protocol.FileAck{
			ID:     fe.ID,
			Status: "error",
			Error:  "unknown transfer",
		}, nil
	}

	// Remove from map immediately — no other handler should touch this transfer
	delete(fr.transfers, fe.ID)
	fr.mu.Unlock()

	// Close the file before rename
	t.file.Close()

	// Verify written bytes match declared size
	if t.written != t.size {
		os.Remove(t.tmpPath)
		return protocol.FileAck{
			ID:     fe.ID,
			Status: "error",
			Error:  fmt.Sprintf("size mismatch: declared %d bytes, received %d", t.size, t.written),
		}, nil
	}

	// Verify hash
	got := hex.EncodeToString(t.hash.Sum(nil))
	if got != fe.SHA256 {
		os.Remove(t.tmpPath)
		return protocol.FileAck{
			ID:     fe.ID,
			Status: "error",
			Error:  fmt.Sprintf("hash mismatch: expected %s, got %s", fe.SHA256, got),
		}, nil
	}

	// Check again that target doesn't exist (race protection)
	if _, err := os.Stat(t.finalPath); err == nil {
		os.Remove(t.tmpPath)
		return protocol.FileAck{
			ID:     fe.ID,
			Status: "error",
			Error:  "file already exists",
		}, nil
	}

	// Rename temp to final
	if err := os.Rename(t.tmpPath, t.finalPath); err != nil {
		os.Remove(t.tmpPath)
		return protocol.FileAck{
			ID:     fe.ID,
			Status: "error",
			Error:  "rename failed: " + err.Error(),
		}, nil
	}

	fr.logger.Info("file transfer complete", "id", fe.ID, "path", t.finalPath, "bytes", t.written)

	return protocol.FileAck{
		ID:           fe.ID,
		Status:       "complete",
		BytesWritten: t.written,
	}, nil
}

// Close cleans up all active transfers.
func (fr *FileReceiver) Close() {
	fr.mu.Lock()
	defer fr.mu.Unlock()
	for id, t := range fr.transfers {
		t.file.Close()
		os.Remove(t.tmpPath)
		delete(fr.transfers, id)
	}
}

func (fr *FileReceiver) cleanStaleLocked() {
	now := time.Now()
	for id, t := range fr.transfers {
		if now.Sub(t.lastAct) > staleTransferTimeout {
			t.file.Close()
			os.Remove(t.tmpPath)
			delete(fr.transfers, id)
			fr.logger.Info("cleaned up stale transfer", "id", id)
		}
	}
}

// validateTransferID ensures the transfer ID is exactly 8 alphanumeric bytes.
func validateTransferID(id string) error {
	if len(id) != fileChunkIDLen {
		return fmt.Errorf("transfer ID must be exactly %d characters", fileChunkIDLen)
	}
	for _, c := range id {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
			return fmt.Errorf("transfer ID contains invalid character: %c", c)
		}
	}
	return nil
}

// validateFilename rejects unsafe filenames.
func validateFilename(name string) error {
	if name == "" {
		return fmt.Errorf("empty filename")
	}
	if strings.HasPrefix(name, ".") {
		return fmt.Errorf("filename cannot start with '.'")
	}
	if strings.Contains(name, "..") {
		return fmt.Errorf("filename cannot contain '..'")
	}
	if strings.ContainsAny(name, "/\\") {
		return fmt.Errorf("filename cannot contain path separators")
	}
	// Only use the base name as extra protection
	if filepath.Base(name) != name {
		return fmt.Errorf("invalid filename")
	}
	return nil
}
