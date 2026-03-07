package relay

import (
	"bufio"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
)

// Blocklist tracks revoked API key IDs loaded from a file.
// The file contains one key ID (jti) per line. Blank and whitespace-only
// lines are ignored. The file is watched for changes via fsnotify so that
// revocations take effect without a server restart.
type Blocklist struct {
	mu      sync.RWMutex
	revoked map[string]struct{}
	path    string
	watcher *fsnotify.Watcher
	done    chan struct{}
}

// NewBlocklist creates a Blocklist that loads revoked key IDs from path.
// If path is empty or the file does not exist, no keys are revoked (this
// is not an error). The file is watched for live updates.
func NewBlocklist(path string) *Blocklist {
	b := &Blocklist{
		revoked: make(map[string]struct{}),
		path:    path,
		done:    make(chan struct{}),
	}

	if path == "" {
		return b
	}

	b.load()

	w, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("blocklist: failed to create watcher: %v", err)
		return b
	}
	b.watcher = w

	if err := w.Add(path); err != nil {
		// File may not exist yet; that's fine.
		log.Printf("blocklist: failed to watch %s: %v", path, err)
	}

	go b.watch()

	return b
}

// IsRevoked returns true if keyID has been revoked.
func (b *Blocklist) IsRevoked(keyID string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	_, ok := b.revoked[keyID]
	return ok
}

// Stop stops the file watcher.
func (b *Blocklist) Stop() {
	if b.watcher != nil {
		b.watcher.Close()
		<-b.done
	}
}

func (b *Blocklist) load() {
	f, err := os.Open(b.path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("blocklist: failed to open %s: %v", b.path, err)
		}
		return
	}
	defer f.Close()

	revoked := make(map[string]struct{})
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			revoked[line] = struct{}{}
		}
	}

	b.mu.Lock()
	b.revoked = revoked
	b.mu.Unlock()
}

func (b *Blocklist) watch() {
	defer close(b.done)
	for {
		select {
		case event, ok := <-b.watcher.Events:
			if !ok {
				return
			}
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
				b.load()
			}
		case err, ok := <-b.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("blocklist: watcher error: %v", err)
		}
	}
}
