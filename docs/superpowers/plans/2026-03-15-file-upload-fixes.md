# File Upload Bug Fixes Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix 8 remaining issues in the file upload feature identified during code review.

**Architecture:** Targeted fixes across Go backend (CLI file receiver, relay handlers, local session) and TypeScript frontend (useWebSocket hook). Each task is independent and can be worked in any order. No new files needed.

**Tech Stack:** Go, TypeScript/React, WebSocket binary protocol

---

## Chunk 1: Go Backend Fixes

### Task 1: Fix partial-write hash divergence

**Files:**
- Modify: `internal/cli/file_receiver.go:185-186`
- Modify: `internal/cli/file_receiver_test.go` (add test)

The bug: `t.hash.Write(data)` hashes the full input buffer, but `t.file.Write(data)` may return `n < len(data)`. The hash then includes bytes not written to disk.

- [ ] **Step 1: Write failing test**

In `internal/cli/file_receiver_test.go`, add a test that verifies only written bytes are hashed. This requires simulating a short write — but since we're writing to a real file and short writes on regular files are extremely unlikely, the fix is structural: hash `data[:n]` instead of `data`. We can verify correctness by checking that the hash at FileEnd matches a manually computed hash of the data actually sent.

The existing tests already verify hash correctness end-to-end. The fix is a one-line change.

- [ ] **Step 2: Fix the hash to only include written bytes**

In `internal/cli/file_receiver.go` line 185, change:

```go
t.hash.Write(data)
```

to:

```go
t.hash.Write(data[:n])
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/cli/ -count=1 -v -run TestFileReceiver`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/cli/file_receiver.go
git commit -m "fix: hash only bytes actually written in file receiver"
```

---

### Task 2: Fix symlink vulnerability in temp file creation

**Files:**
- Modify: `internal/cli/file_receiver.go:111`
- Modify: `internal/cli/file_receiver_test.go` (add test)

The bug: `os.Create(tmpPath)` follows symlinks. A pre-existing symlink at the temp path could redirect writes outside `destDir`.

- [ ] **Step 1: Write failing test**

Add to `internal/cli/file_receiver_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -count=1 -v -run TestFileReceiverRejectsSymlinkTmpPath`
Expected: FAIL — `os.Create` follows the symlink and succeeds

- [ ] **Step 3: Replace os.Create with os.OpenFile using O_EXCL**

In `internal/cli/file_receiver.go`, replace line 111:

```go
f, err := os.Create(tmpPath)
```

with:

```go
f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
```

`O_EXCL` ensures the call fails if the path already exists (including as a symlink).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/ -count=1 -v -run TestFileReceiverRejectsSymlinkTmpPath`
Expected: PASS

- [ ] **Step 5: Run all file receiver tests**

Run: `go test ./internal/cli/ -count=1 -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/cli/file_receiver.go internal/cli/file_receiver_test.go
git commit -m "fix: use O_EXCL for temp file creation to prevent symlink attacks"
```

---

### Task 3: Fix misleading "Reject duplicate" comment

**Files:**
- Modify: `internal/cli/file_receiver.go:91`

- [ ] **Step 1: Fix the comment**

Change line 91 from:

```go
// Reject duplicate transfer IDs
```

to:

```go
// Replace duplicate transfer IDs (clean up old transfer first)
```

- [ ] **Step 2: Commit**

```bash
git add internal/cli/file_receiver.go
git commit -m "fix: correct misleading comment about duplicate transfer ID handling"
```

---

### Task 4: Send error ack when uploads are disabled

**Files:**
- Modify: `internal/cli/app.go:438-467`

The bug: when `fileRecv == nil`, file messages are silently dropped. The viewer hangs 30s waiting for ack.

- [ ] **Step 1: Modify the FileStart case to send an error ack when uploads are disabled**

In `internal/cli/app.go`, replace the three `fileRecv == nil` blocks. For `TypeFileStart` (around line 438):

```go
case protocol.TypeFileStart:
    if fileRecv == nil {
        // Uploads disabled — send error ack so the viewer fails fast
        var fs protocol.FileStart
        if err := protocol.DecodeJSON(pl, &fs); err == nil {
            ack := protocol.FileAck{
                ID:     fs.ID,
                Status: "error",
                Error:  "uploads disabled on this session",
            }
            ws.Send(connCtx, protocol.TypeFileAck, ack)
        }
        continue
    }
```

The `TypeFileChunk` and `TypeFileEnd` cases can remain as `continue` — once the viewer receives the error ack from FileStart, it will stop sending. Chunks/ends for an unknown transfer will be ignored.

- [ ] **Step 2: Run tests**

Run: `go test ./internal/cli/ -count=1 -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/cli/app.go
git commit -m "fix: send error ack when file uploads are disabled instead of silent drop"
```

---

### Task 5: Remove FileAck broadcast fallback in CLI handler

**Files:**
- Modify: `internal/relay/handler_ws_cli.go:260-277`

The bug: if `DecodeJSON` or `GetLocal` fails, code falls through to `BroadcastOutput`, sending ack to ALL viewers and skipping `CleanupFileTransfer`.

- [ ] **Step 1: Replace the fallback broadcast with a log-and-drop**

In `internal/relay/handler_ws_cli.go`, replace the `TypeFileAck` case (lines 260-277):

```go
case protocol.TypeFileAck:
    var ack protocol.FileAck
    if err := protocol.DecodeJSON(payload, &ack); err != nil {
        s.logger.Warn("failed to decode FileAck", "session", sessionID, "err", err)
        break
    }
    msg, err := protocol.Encode(protocol.TypeFileAck, payload)
    if err != nil {
        break
    }
    ls, ok := s.hub.GetLocal(sessionID)
    if !ok {
        s.logger.Warn("no local session for FileAck", "session", sessionID)
        break
    }
    ls.SendFileAck(ctx, ack.ID, msg)
    if ack.Status == "complete" || ack.Status == "error" {
        ls.CleanupFileTransfer(ack.ID)
    }
```

Wait — `protocol.Encode(protocol.TypeFileAck, payload)` won't work because `payload` is `[]byte` and FileAck is a JSON type. The original code manually constructs the frame. Let's keep the manual construction but remove the fallback:

```go
case protocol.TypeFileAck:
    var ack protocol.FileAck
    if err := protocol.DecodeJSON(payload, &ack); err != nil {
        s.logger.Warn("failed to decode FileAck", "session", sessionID, "err", err)
        break
    }
    msg := make([]byte, 1+len(payload))
    msg[0] = protocol.TypeFileAck
    copy(msg[1:], payload)
    ls, ok := s.hub.GetLocal(sessionID)
    if !ok {
        s.logger.Warn("no local session for FileAck", "session", sessionID)
        break
    }
    ls.SendFileAck(ctx, ack.ID, msg)
    if ack.Status == "complete" || ack.Status == "error" {
        ls.CleanupFileTransfer(ack.ID)
    }
```

- [ ] **Step 2: Run relay tests**

Run: `go test ./internal/relay/ -count=1 -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/relay/handler_ws_cli.go
git commit -m "fix: remove FileAck broadcast fallback, log and drop on parse/lookup failure"
```

---

### Task 6: Clean up fileTransferOwners on viewer disconnect

**Files:**
- Modify: `internal/relay/local_session.go` (add `CleanupViewerTransfers` method)
- Modify: `internal/relay/handler_ws_viewer.go:92-93` (call cleanup in defer)

The bug: `fileTransferOwners` entries are never cleaned up when a viewer disconnects mid-transfer.

- [ ] **Step 1: Add CleanupViewerTransfers method to LocalSession**

In `internal/relay/local_session.go`, add after `CleanupFileTransfer` (after line 148):

```go
// CleanupViewerTransfers removes all file transfer mappings for a given viewer.
func (ls *LocalSession) CleanupViewerTransfers(viewerID string) {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	for transferID, ownerID := range ls.fileTransferOwners {
		if ownerID == viewerID {
			delete(ls.fileTransferOwners, transferID)
		}
	}
}
```

- [ ] **Step 2: Call CleanupViewerTransfers in the viewer disconnect defer**

In `internal/relay/handler_ws_viewer.go`, in the defer block (line 92-93), add a call before `RemoveViewer`:

```go
defer func() {
    ls.CleanupViewerTransfers(viewerID)
    ls.RemoveViewer(viewerID)
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/relay/ -count=1 -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/relay/local_session.go internal/relay/handler_ws_viewer.go
git commit -m "fix: clean up file transfer owner mappings when viewer disconnects"
```

---

## Chunk 2: Frontend Fixes

### Task 7: Fix SHA256 memory blowup on large files

**Files:**
- Modify: `web/src/hooks/useWebSocket.ts:292-330`

The bug: all chunks are accumulated in `hashParts[]` then concatenated into a single `Uint8Array` (~200MB for 100MB file). The Web Crypto API's `crypto.subtle.digest` doesn't support streaming, but we can hash each chunk as we read it by using a simple incremental approach — hash chunks one at a time using a manually maintained state, or more practically, accumulate chunks but hash them *as we go* using a library. Since we can't add dependencies easily, and the `crypto.subtle` API doesn't support incremental hashing, the pragmatic fix is to hash the file separately by reading it once more via `file.slice()` — but that doubles I/O.

The best approach: remove the `hashParts` accumulation entirely and compute the hash by reading the file a second time after sending all chunks. This uses file.slice() to read sequentially, feeding the entire file to `crypto.subtle.digest` in one pass via a ReadableStream → ArrayBuffer. The key insight is that `file.arrayBuffer()` already streams efficiently from disk through the browser's blob implementation — we just need to avoid *also* keeping all chunks in memory.

- [ ] **Step 1: Replace the hashParts accumulation with a post-send hash computation**

In `web/src/hooks/useWebSocket.ts`, replace the chunk-sending loop and hash computation (lines 292-330):

```typescript
      // Stream file in chunks using file.slice() to avoid loading entire file into memory
      const textEncoder = new TextEncoder();
      const idBytes = textEncoder.encode(id); // 8 ASCII bytes

      let offset = 0;
      while (offset < file.size) {
        if (ws.readyState !== WebSocket.OPEN) {
          markError("connection lost during upload");
          return;
        }
        const end = Math.min(offset + FILE_CHUNK_SIZE, file.size);
        const blob = file.slice(offset, end);
        const chunkBuffer = await blob.arrayBuffer();
        const chunk = new Uint8Array(chunkBuffer);

        // Build FileChunk payload: [8-byte ID][raw data]
        const chunkPayload = new Uint8Array(TRANSFER_ID_LEN + chunk.length);
        chunkPayload.set(idBytes, 0);
        chunkPayload.set(chunk, TRANSFER_ID_LEN);

        ws.send(encode(MsgType.FileChunk, chunkPayload));
        offset = end;
      }

      // Compute SHA256 from the original file (reads from disk, not memory)
      const fileBuffer = await file.arrayBuffer();
      const hashBuffer = await crypto.subtle.digest("SHA-256", fileBuffer);
      const hashArray = new Uint8Array(hashBuffer);
      const sha256 = Array.from(hashArray)
        .map((b) => b.toString(16).padStart(2, "0"))
        .join("");
```

This removes the `hashParts` array entirely. The file is read from disk twice (once for chunked sending, once for hashing) but only one copy is in memory at a time. For the 100MB max this means ~100MB peak instead of ~200MB. The browser's blob implementation handles the second read efficiently from its file cache.

Note: A truly streaming hash would require a WASM SHA-256 implementation (Web Crypto has no incremental API). This is a reasonable trade-off for the current max file size of 100MB.

- [ ] **Step 2: Update the comment about streaming**

Update the comment above the hash computation to be accurate:

```typescript
      // Compute SHA256 by re-reading the file from disk.
      // Web Crypto's digest() has no incremental API, so we read the file
      // a second time rather than accumulating all chunks in memory.
```

- [ ] **Step 3: Run frontend tests**

Run: `cd web && npx vitest run --reporter=verbose`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add web/src/hooks/useWebSocket.ts
git commit -m "fix: eliminate double-buffering in SHA256 computation for file uploads"
```

---

### Task 8: Add FileEnd completion timeout

**Files:**
- Modify: `web/src/hooks/useWebSocket.ts:332-336`

The bug: after sending FileEnd, there's no timeout if the final `complete`/`error` ack never arrives. The transfer stays stuck as "uploading" in the UI.

- [ ] **Step 1: Add a completion timeout after FileEnd**

After the `ws.send(encode(MsgType.FileEnd, ...))` line, add a timeout that waits for the final ack:

```typescript
      // Send FileEnd
      ws.send(encode(MsgType.FileEnd, { id, sha256 }));

      // Wait for completion ack with timeout
      const completeAck = await new Promise<FileAckPayload>((resolve) => {
        pendingAcksRef.current.set(id, resolve);
        setTimeout(() => {
          if (pendingAcksRef.current.has(id)) {
            pendingAcksRef.current.delete(id);
            resolve({ id, status: "error", error: "completion ack timeout" });
          }
        }, ACK_TIMEOUT_MS);
      });

      if (completeAck.status === "error") {
        markError(completeAck.error ?? "upload failed");
        return;
      }

      // Schedule cleanup after successful completion
      scheduleTransferCleanup(id);
```

This replaces the existing `scheduleTransferCleanup(id)` call on line 336 which fired immediately without waiting for the ack.

- [ ] **Step 2: Run frontend tests**

Run: `cd web && npx vitest run --reporter=verbose`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add web/src/hooks/useWebSocket.ts
git commit -m "fix: add completion timeout after FileEnd to prevent stuck transfers"
```

---

## Chunk 3: Comment Fixes

### Task 9: Update stale codec/protocol comments

**Files:**
- Modify: `internal/protocol/codec.go:12`
- Modify: `web/src/lib/protocol.ts:79`

- [ ] **Step 1: Fix Go codec comment**

In `internal/protocol/codec.go` line 12, change:

```go
// For data types (Stdout/Stdin) payload is raw bytes.
```

to:

```go
// For data types (Stdout/Stdin/FileChunk) payload is raw bytes.
```

- [ ] **Step 2: Fix TypeScript encode JSDoc**

In `web/src/lib/protocol.ts` line 79, change:

```typescript
 * For Stdin: payload is Uint8Array (raw key bytes).
```

to:

```typescript
 * For Stdin/FileChunk: payload is Uint8Array (raw bytes).
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/protocol/ -count=1 -v`
Run: `cd web && npx vitest run --reporter=verbose`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/protocol/codec.go web/src/lib/protocol.ts
git commit -m "fix: update codec comments to include FileChunk as raw-bytes type"
```
