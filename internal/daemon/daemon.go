package daemon

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/fsnotify/fsnotify"

	"github.com/brporter/phosphor/internal/protocol"
)

// PTYProcess is the interface for a pseudo-terminal process.
type PTYProcess interface {
	io.ReadWriteCloser
	Resize(cols, rows int) error
	Wait(ctx context.Context) (int, error)
	Pid() int
}

// SpawnFunc is the function signature for spawning a PTY as a local user.
// Implemented per-platform in spawn_unix.go and spawn_windows.go.
type SpawnFunc func(shell string, localUser string) (PTYProcess, int, int, error)

// backoff durations for reconnect attempts.
var backoffSchedule = []time.Duration{
	1 * time.Second,
	2 * time.Second,
	4 * time.Second,
	8 * time.Second,
	16 * time.Second,
	30 * time.Second,
}

// keepalive constants
const (
	pingInterval = 30 * time.Second // how often to send pings
	pongTimeout  = 15 * time.Second // how long to wait for pong after ping
)

// Daemon manages persistent relay connections for identity mappings.
type Daemon struct {
	Config     *Config
	Token      string
	Logger     *slog.Logger
	ConfigPath string
	Spawn      SpawnFunc // injected; platform-specific

	mu       sync.Mutex
	sessions map[string]*managedSession
}

type managedSession struct {
	mapping        Mapping
	sessionID      string
	reconnectToken string
	cancel         context.CancelFunc

	mu      sync.Mutex
	proc    PTYProcess
	stdinCh chan []byte
}

// Run starts one goroutine per mapping, watches the config file for changes,
// and waits for all to finish when ctx is cancelled.
func (d *Daemon) Run(ctx context.Context) {
	d.mu.Lock()
	d.sessions = make(map[string]*managedSession)
	d.mu.Unlock()

	var wg sync.WaitGroup

	d.startMappings(ctx, &wg, d.Config.Mappings)

	// Start config watcher in background
	if d.ConfigPath != "" {
		go d.watchConfig(ctx, &wg)
	}

	wg.Wait()
}

// startMappings launches a runMapping goroutine for each mapping.
func (d *Daemon) startMappings(ctx context.Context, wg *sync.WaitGroup, mappings []Mapping) {
	for _, m := range mappings {
		mCtx, mCancel := context.WithCancel(ctx)
		ms := &managedSession{mapping: m, cancel: mCancel}
		d.setSession(m.Identity, ms)

		wg.Add(1)
		go func(mapping Mapping) {
			defer wg.Done()
			d.runMapping(mCtx, mapping)
		}(m)
	}
}

// watchConfig watches the config file for changes and adds/removes mappings.
func (d *Daemon) watchConfig(ctx context.Context, wg *sync.WaitGroup) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		d.Logger.Warn("config watch unavailable", "err", err)
		return
	}
	defer watcher.Close()

	if err := watcher.Add(d.ConfigPath); err != nil {
		d.Logger.Warn("failed to watch config", "path", d.ConfigPath, "err", err)
		return
	}

	d.Logger.Info("watching config file", "path", d.ConfigPath)

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if event.Op&(fsnotify.Write|fsnotify.Create) != 0 {
				d.reloadConfig(ctx, wg)
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			d.Logger.Warn("config watch error", "err", err)
		}
	}
}

// reloadConfig reads the config file and diffs mappings.
func (d *Daemon) reloadConfig(ctx context.Context, wg *sync.WaitGroup) {
	newCfg, err := ReadConfig(d.ConfigPath)
	if err != nil {
		d.Logger.Error("reload config failed", "err", err)
		return
	}

	// Build sets of current and new identities
	newMap := make(map[string]Mapping)
	for _, m := range newCfg.Mappings {
		newMap[m.Identity] = m
	}

	oldMap := make(map[string]Mapping)
	d.mu.Lock()
	for id, ms := range d.sessions {
		oldMap[id] = ms.mapping
	}
	d.mu.Unlock()

	// Stop removed mappings
	for id := range oldMap {
		if _, exists := newMap[id]; !exists {
			d.Logger.Info("removing mapping", "identity", id)
			d.mu.Lock()
			if ms, ok := d.sessions[id]; ok {
				ms.cancel()
				delete(d.sessions, id)
			}
			d.mu.Unlock()
		}
	}

	// Start new mappings
	var toStart []Mapping
	for id, m := range newMap {
		if _, exists := oldMap[id]; !exists {
			d.Logger.Info("adding mapping", "identity", id)
			toStart = append(toStart, m)
		}
	}

	if len(toStart) > 0 {
		d.startMappings(ctx, wg, toStart)
	}

	d.Config = newCfg
	d.Logger.Info("config reloaded", "mappings", len(newCfg.Mappings))
}

// isAuthError returns true if the error indicates an authentication failure.
func isAuthError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "server error: auth_failed") ||
		strings.Contains(msg, "server error: invalid_token")
}

// runMapping is the reconnect loop for a single mapping with exponential backoff.
func (d *Daemon) runMapping(ctx context.Context, mapping Mapping) {
	attempt := 0
	authFailures := 0
	const maxAuthFailures = 3

	for {
		if ctx.Err() != nil {
			return
		}

		connStart := time.Now()
		err := d.runConnection(ctx, mapping)
		if ctx.Err() != nil {
			return
		}

		if err != nil {
			if isAuthError(err) {
				authFailures++
				d.Logger.Error("authentication failed",
					"identity", mapping.Identity,
					"attempt", authFailures,
					"max", maxAuthFailures,
					"err", err,
				)
				if authFailures >= maxAuthFailures {
					d.Logger.Error("max auth failures reached, stopping mapping",
						"identity", mapping.Identity,
					)
					return
				}
			} else {
				authFailures = 0
			}

			d.Logger.Warn("connection failed",
				"identity", mapping.Identity,
				"attempt", attempt,
				"err", err,
			)
		}

		// Reset backoff if the connection was alive long enough to have
		// completed at least one keepalive cycle — this means it was a
		// genuine session, not a connect-and-immediately-fail loop.
		if time.Since(connStart) > pingInterval {
			attempt = 0
		}

		delay := backoffSchedule[min(attempt, len(backoffSchedule)-1)]
		attempt++

		d.Logger.Info("reconnecting",
			"identity", mapping.Identity,
			"delay", delay,
			"attempt", attempt,
		)

		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return
		}
	}
}

// runConnection establishes a single WebSocket connection to the relay,
// performs the Hello/Welcome handshake, and enters the read loop.
func (d *Daemon) runConnection(ctx context.Context, mapping Mapping) error {
	ms := d.getSession(mapping.Identity)
	if ms == nil {
		return fmt.Errorf("no managed session for %s", mapping.Identity)
	}

	wsURL := d.Config.Relay + "/ws/cli"
	d.Logger.Info("connecting to relay",
		"url", wsURL,
		"identity", mapping.Identity,
	)

	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		Subprotocols: []string{"phosphor"},
	})
	if err != nil {
		return fmt.Errorf("dial %s: %w", wsURL, err)
	}
	conn.SetReadLimit(1 << 20) // 1MB
	defer conn.Close(websocket.StatusNormalClosure, "goodbye")

	// Build Hello
	hostname, _ := os.Hostname()
	hello := protocol.Hello{
		Token:       d.Token,
		Mode:        "pty",
		Command:     mapping.Shell,
		Hostname:    hostname,
		Lazy:        true,
		DelegateFor: mapping.Identity,
	}

	// Include reconnect info if we have it
	ms.mu.Lock()
	if ms.sessionID != "" {
		hello.SessionID = ms.sessionID
		hello.ReconnectToken = ms.reconnectToken
	}
	ms.mu.Unlock()

	// Send Hello
	helloData, err := protocol.Encode(protocol.TypeHello, hello)
	if err != nil {
		return fmt.Errorf("encode hello: %w", err)
	}
	if err := conn.Write(ctx, websocket.MessageBinary, helloData); err != nil {
		return fmt.Errorf("send hello: %w", err)
	}

	// Read Welcome
	_, welcomeData, err := conn.Read(ctx)
	if err != nil {
		return fmt.Errorf("receive welcome: %w", err)
	}
	msgType, payload, err := protocol.Decode(welcomeData)
	if err != nil {
		return fmt.Errorf("decode welcome: %w", err)
	}
	if msgType == protocol.TypeError {
		var e protocol.Error
		protocol.DecodeJSON(payload, &e)
		return fmt.Errorf("server error: %s: %s", e.Code, e.Message)
	}
	if msgType != protocol.TypeWelcome {
		return fmt.Errorf("unexpected message type: 0x%02x", msgType)
	}
	var welcome protocol.Welcome
	if err := protocol.DecodeJSON(payload, &welcome); err != nil {
		return fmt.Errorf("decode welcome: %w", err)
	}

	// Store session info
	ms.mu.Lock()
	ms.sessionID = welcome.SessionID
	ms.reconnectToken = welcome.ReconnectToken
	ms.mu.Unlock()

	d.Logger.Info("session active",
		"identity", mapping.Identity,
		"session_id", welcome.SessionID,
		"view_url", welcome.ViewURL,
	)

	// Start keepalive ping goroutine — sends periodic pings to detect dead
	// connections and keep the WebSocket alive through NAT/firewalls.
	connCtx, connCancel := context.WithCancel(ctx)
	defer connCancel()

	go d.keepalive(connCtx, conn, mapping.Identity)

	return d.readLoop(connCtx, conn, mapping)
}

// readLoop reads messages from the relay and dispatches them.
func (d *Daemon) readLoop(ctx context.Context, conn *websocket.Conn, mapping Mapping) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		_, data, err := conn.Read(ctx)
		if err != nil {
			return fmt.Errorf("read: %w", err)
		}

		msgType, payload, err := protocol.Decode(data)
		if err != nil {
			d.Logger.Warn("decode error", "identity", mapping.Identity, "err", err)
			continue
		}

		switch msgType {
		case protocol.TypeSpawnRequest:
			go d.handleSpawn(ctx, conn, mapping)

		case protocol.TypeStdin:
			d.forwardStdin(mapping.Identity, payload)

		case protocol.TypeResize:
			var sz protocol.Resize
			if err := protocol.DecodeJSON(payload, &sz); err == nil {
				d.resizePTY(mapping.Identity, sz.Cols, sz.Rows)
			}

		case protocol.TypeRestart:
			// Restart is treated the same as SpawnRequest (respawn)
			go d.handleSpawn(ctx, conn, mapping)

		case protocol.TypePing:
			pongData, err := protocol.Encode(protocol.TypePong, nil)
			if err == nil {
				conn.Write(ctx, websocket.MessageBinary, pongData)
			}

		case protocol.TypeEnd:
			d.Logger.Info("session ended by relay", "identity", mapping.Identity)
			return nil

		case protocol.TypeViewerCount:
			var vc protocol.ViewerCount
			if err := protocol.DecodeJSON(payload, &vc); err == nil {
				d.Logger.Debug("viewer count", "identity", mapping.Identity, "count", vc.Count)
			}

		default:
			d.Logger.Debug("unhandled message type",
				"identity", mapping.Identity,
				"type", fmt.Sprintf("0x%02x", msgType),
			)
		}
	}
}

// keepalive sends periodic pings to detect dead connections and keep the
// WebSocket alive through NAT/firewalls. If a ping write fails, it cancels
// the connection context so the read loop exits.
func (d *Daemon) keepalive(ctx context.Context, conn *websocket.Conn, identity string) {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pingData, err := protocol.Encode(protocol.TypePing, nil)
			if err != nil {
				continue
			}

			// Use a short write deadline so we don't block forever
			writeCtx, cancel := context.WithTimeout(ctx, pongTimeout)
			err = conn.Write(writeCtx, websocket.MessageBinary, pingData)
			cancel()

			if err != nil {
				d.Logger.Warn("keepalive ping failed, connection likely dead",
					"identity", identity,
					"err", err,
				)
				// Force-close the connection so readLoop returns an error
				conn.CloseNow()
				return
			}
		}
	}
}

// handleSpawn spawns a PTY process for the given mapping and bridges I/O.
func (d *Daemon) handleSpawn(ctx context.Context, conn *websocket.Conn, mapping Mapping) {
	ms := d.getSession(mapping.Identity)
	if ms == nil {
		d.Logger.Error("no managed session for spawn", "identity", mapping.Identity)
		return
	}

	// Check if already spawned
	ms.mu.Lock()
	if ms.proc != nil {
		ms.mu.Unlock()
		d.Logger.Warn("spawn requested but process already running", "identity", mapping.Identity)
		return
	}
	ms.mu.Unlock()

	if d.Spawn == nil {
		d.Logger.Error("no spawn function configured", "identity", mapping.Identity)
		d.sendProcessExited(ctx, conn, -1)
		return
	}

	// Spawn PTY
	proc, cols, rows, err := d.Spawn(mapping.Shell, mapping.LocalUser)
	if err != nil {
		d.Logger.Error("spawn failed", "identity", mapping.Identity, "err", err)
		d.sendProcessExited(ctx, conn, -1)
		return
	}

	d.Logger.Info("process spawned",
		"identity", mapping.Identity,
		"pid", proc.Pid(),
		"cols", cols,
		"rows", rows,
	)

	// Store proc and create stdinCh
	ms.mu.Lock()
	ms.proc = proc
	ms.stdinCh = make(chan []byte, 256)
	stdinCh := ms.stdinCh
	ms.mu.Unlock()

	// Send SpawnComplete
	sc := protocol.SpawnComplete{Cols: cols, Rows: rows}
	scData, err := protocol.Encode(protocol.TypeSpawnComplete, sc)
	if err == nil {
		conn.Write(ctx, websocket.MessageBinary, scData)
	}

	// Start stdout goroutine: read from PTY, encode as TypeStdout, write to WS
	stdoutDone := make(chan struct{})
	go func() {
		defer close(stdoutDone)
		buf := make([]byte, 32*1024)
		for {
			n, readErr := proc.Read(buf)
			if n > 0 {
				outData, encErr := protocol.Encode(protocol.TypeStdout, buf[:n])
				if encErr != nil {
					d.Logger.Error("encode stdout", "identity", mapping.Identity, "err", encErr)
					return
				}
				if writeErr := conn.Write(ctx, websocket.MessageBinary, outData); writeErr != nil {
					d.Logger.Debug("send stdout failed", "identity", mapping.Identity, "err", writeErr)
					return
				}
			}
			if readErr != nil {
				if readErr != io.EOF {
					d.Logger.Debug("pty read ended", "identity", mapping.Identity, "err", readErr)
				}
				return
			}
		}
	}()

	// Start stdin goroutine: read from stdinCh, write to PTY
	stdinDone := make(chan struct{})
	go func() {
		defer close(stdinDone)
		for data := range stdinCh {
			if _, err := proc.Write(data); err != nil {
				d.Logger.Debug("pty write failed", "identity", mapping.Identity, "err", err)
				return
			}
		}
	}()

	// Wait for process exit
	exitCode, waitErr := proc.Wait(ctx)
	if waitErr != nil {
		d.Logger.Warn("process wait error", "identity", mapping.Identity, "err", waitErr)
	} else {
		d.Logger.Info("process exited", "identity", mapping.Identity, "exit_code", exitCode)
	}

	// Clean up
	proc.Close()

	ms.mu.Lock()
	ms.proc = nil
	if ms.stdinCh != nil {
		close(ms.stdinCh)
		ms.stdinCh = nil
	}
	ms.mu.Unlock()

	// Wait for stdout goroutine to finish flushing
	<-stdoutDone

	// Send ProcessExited
	d.sendProcessExited(ctx, conn, exitCode)
}

// sendProcessExited sends a ProcessExited message over the connection.
func (d *Daemon) sendProcessExited(ctx context.Context, conn *websocket.Conn, exitCode int) {
	pe := protocol.ProcessExited{ExitCode: exitCode}
	data, err := protocol.Encode(protocol.TypeProcessExited, pe)
	if err != nil {
		d.Logger.Error("encode process exited", "err", err)
		return
	}
	if err := conn.Write(ctx, websocket.MessageBinary, data); err != nil {
		d.Logger.Debug("send process exited failed", "err", err)
	}
}

// forwardStdin sends data to the stdin channel of the managed session (non-blocking, drop if full).
func (d *Daemon) forwardStdin(identity string, data []byte) {
	ms := d.getSession(identity)
	if ms == nil {
		return
	}
	ms.mu.Lock()
	ch := ms.stdinCh
	ms.mu.Unlock()

	if ch == nil {
		return
	}

	// Make a copy since the caller's buffer may be reused
	copied := make([]byte, len(data))
	copy(copied, data)

	select {
	case ch <- copied:
	default:
		// Drop if channel is full
	}
}

// resizePTY resizes the PTY of the managed session.
func (d *Daemon) resizePTY(identity string, cols, rows int) {
	ms := d.getSession(identity)
	if ms == nil {
		return
	}
	ms.mu.Lock()
	proc := ms.proc
	ms.mu.Unlock()

	if proc != nil {
		if err := proc.Resize(cols, rows); err != nil {
			d.Logger.Debug("resize failed", "identity", identity, "err", err)
		}
	}
}

// getSession returns the managed session for the given identity.
func (d *Daemon) getSession(identity string) *managedSession {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.sessions[identity]
}

// setSession stores a managed session for the given identity.
func (d *Daemon) setSession(identity string, ms *managedSession) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.sessions == nil {
		d.sessions = make(map[string]*managedSession)
	}
	d.sessions[identity] = ms
}
