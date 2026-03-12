package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/brporter/phosphor/internal/protocol"
	"golang.org/x/term"
)

// App orchestrates the CLI: connects to the relay, spawns the process, and bridges I/O.
type App struct {
	Config  Config
	Token   string
	Logger  *slog.Logger
	Command []string // for PTY mode
	Mode    string   // "pty" or "pipe"
	Restart string   // "manual", "auto", "never"
}

var errProcessExited = errors.New("process exited")

// backoff durations for reconnect attempts.
var backoffSchedule = []time.Duration{
	1 * time.Second,
	2 * time.Second,
	4 * time.Second,
	8 * time.Second,
	16 * time.Second,
	30 * time.Second,
}

const (
	cliPingInterval = 30 * time.Second
	cliPingTimeout  = 15 * time.Second
)

// connectionResult is the structured result from a single connection attempt.
type connectionResult struct {
	err           error
	processExited bool
	exitCode      int
	restarted     bool // true if we received TypeRestart (manual mode)
	authFailed    bool // relay rejected our token
}

// Run starts the CLI session with restart support.
func (a *App) Run(ctx context.Context) error {
	appCtx, appCancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer appCancel()

	var sessionID, reconnectToken string

	for {
		err := a.runWithProcess(appCtx, appCancel, &sessionID, &reconnectToken)
		if appCtx.Err() != nil {
			a.Logger.Info("session ended")
			return nil
		}
		if !errors.Is(err, errProcessExited) {
			// Not a process exit — either success or fatal error
			if err != nil {
				return err
			}
			return nil
		}

		// Process exited — check restart mode
		switch a.Restart {
		case "auto":
			a.Logger.Info("process exited, restarting automatically")
			fmt.Fprintf(os.Stderr, "Process exited. Restarting...\r\n")
		case "manual":
			// runWithProcess waited for TypeRestart and returned errProcessExited
			a.Logger.Info("restarting process after manual trigger")
		default:
			// "never" returns nil from runWithProcess, so we won't reach here
			a.Logger.Info("session ended")
			return nil
		}
	}
}

// runWithProcess manages the PTY lifecycle and reconnection loop for a single process.
func (a *App) runWithProcess(appCtx context.Context, appCancel context.CancelFunc, sessionID, reconnectToken *string) error {
	var cols, rows int
	var proc io.ReadWriteCloser
	var ptyProc PTYProcess

	processExited := make(chan int, 1)

	if a.Mode == "pty" {
		p, c, r, err := StartPTY(a.Command)
		if err != nil {
			return fmt.Errorf("start pty: %w", err)
		}
		defer p.Close()
		proc = p
		ptyProc = p
		cols = c
		rows = r

		a.Logger.Info("process started", "pid", p.Pid(), "command", strings.Join(a.Command, " "))

		// Enter raw terminal mode so keystrokes pass through to the PTY
		if fd := int(os.Stdin.Fd()); term.IsTerminal(fd) {
			oldState, err := term.MakeRaw(fd)
			if err == nil {
				defer term.Restore(fd, oldState)
			} else {
				a.Logger.Warn("failed to set raw terminal mode", "err", err)
			}
		}

		// Local stdin → PTY
		go func() {
			buf := make([]byte, 1024)
			for {
				n, err := os.Stdin.Read(buf)
				if n > 0 {
					proc.Write(buf[:n])
					// Ctrl+C (0x03) in raw mode: shut down phosphor
					if bytes.IndexByte(buf[:n], 0x03) >= 0 {
						appCancel()
						return
					}
				}
				if err != nil {
					return
				}
			}
		}()

		// Watch for local terminal resize (SIGWINCH on Unix, no-op on Windows)
		go watchTerminalResize(appCtx, p)

		go func() {
			exitCode, waitErr := p.Wait(appCtx)
			if waitErr != nil {
				a.Logger.Warn("process wait error", "pid", p.Pid(), "err", waitErr)
			} else {
				a.Logger.Info("process exited", "pid", p.Pid(), "exit_code", exitCode)
			}
			processExited <- exitCode
		}()
	} else {
		proc = NewPipeReader(os.Stdin)
		cols = 80
		rows = 24
	}

	attempt := 0

	for {
		result := a.runConnection(appCtx, proc, ptyProc, cols, rows, sessionID, reconnectToken, processExited)

		if result.processExited {
			switch a.Restart {
			case "manual":
				if result.restarted {
					return errProcessExited
				}
				// Connection lost while waiting for restart, or no ws
				return nil
			case "auto":
				return errProcessExited
			default: // "never" or unset
				return nil
			}
		}

		if result.err == nil || appCtx.Err() != nil {
			return nil
		}

		// Auth rejected — try browser login once, then retry
		if result.authFailed && attempt == 0 {
			fmt.Fprintf(os.Stderr, "Authentication required.\r\n")
			idToken, err := BrowserLogin(appCtx, a.Config.RelayURL)
			if err != nil {
				return fmt.Errorf("login failed: %w", err)
			}
			if err := SaveTokenCache(&TokenCache{AccessToken: idToken}); err != nil {
				a.Logger.Warn("failed to cache token", "err", err)
			}
			a.Token = idToken
			attempt++
			continue
		}

		// Connection lost — retry with backoff
		delay := backoffSchedule[min(attempt, len(backoffSchedule)-1)]
		attempt++
		fmt.Fprintf(os.Stderr, "Relay disconnected. Reconnecting in %s...\r\n", delay)
		a.Logger.Info("relay disconnected, will retry", "delay", delay, "attempt", attempt)

		select {
		case <-time.After(delay):
		case <-appCtx.Done():
			return nil
		}
	}
}

// runConnection establishes a single WebSocket connection to the relay,
// performs the Hello/Welcome handshake, and bridges I/O until the connection drops.
// If the process exits, runConnection handles the ProcessExited/Restart exchange
// itself (keeping the ws alive), then returns the result.
func (a *App) runConnection(
	appCtx context.Context,
	proc io.ReadWriteCloser,
	ptyProc PTYProcess,
	cols, rows int,
	sessionID, reconnectToken *string,
	processExited <-chan int,
) connectionResult {
	a.Logger.Info("connecting to relay", "url", a.Config.RelayURL)

	wsURL := a.Config.RelayURL + "/ws/cli"
	ws, err := ConnectWebSocket(appCtx, wsURL)
	if err != nil {
		return connectionResult{err: fmt.Errorf("connect to relay: %w", err)}
	}
	defer ws.Close()

	// Send Hello
	hostname, _ := os.Hostname()
	hello := protocol.Hello{
		Token:          a.Token,
		Mode:           a.Mode,
		Cols:           cols,
		Rows:           rows,
		Command:        strings.Join(a.Command, " "),
		Hostname:       hostname,
		SessionID:      *sessionID,
		ReconnectToken: *reconnectToken,
	}
	if err := ws.Send(appCtx, protocol.TypeHello, hello); err != nil {
		return connectionResult{err: fmt.Errorf("send hello: %w", err)}
	}

	// Read Welcome
	msgType, payload, err := ws.Receive(appCtx)
	if err != nil {
		return connectionResult{err: fmt.Errorf("receive welcome: %w", err)}
	}
	if msgType == protocol.TypeError {
		var e protocol.Error
		protocol.DecodeJSON(payload, &e)
		return connectionResult{
			err:        fmt.Errorf("server error: %s: %s", e.Code, e.Message),
			authFailed: e.Code == "auth_failed",
		}
	}
	if msgType != protocol.TypeWelcome {
		return connectionResult{err: fmt.Errorf("unexpected message type: 0x%02x", msgType)}
	}
	var welcome protocol.Welcome
	if err := protocol.DecodeJSON(payload, &welcome); err != nil {
		return connectionResult{err: fmt.Errorf("decode welcome: %w", err)}
	}

	*sessionID = welcome.SessionID
	*reconnectToken = welcome.ReconnectToken

	if hello.SessionID == "" {
		fmt.Fprintf(os.Stderr, "Session live: %s\r\n", welcome.ViewURL)
	} else {
		fmt.Fprintf(os.Stderr, "Reconnected: %s\r\n", welcome.ViewURL)
	}
	a.Logger.Info("session active", "session_id", welcome.SessionID, "url", welcome.ViewURL)

	// connCtx scoped to this single connection.
	connCtx, connCancel := context.WithCancel(appCtx)
	defer connCancel()

	procDead := make(chan struct{})
	connLost := make(chan struct{})
	sessionDestroyed := make(chan struct{})
	restartCh := make(chan struct{}, 1)
	var procDeadOnce sync.Once
	var connLostOnce sync.Once
	var destroyedOnce sync.Once
	closeProcDead := func() { procDeadOnce.Do(func() { close(procDead) }) }
	closeConnLost := func() { connLostOnce.Do(func() { close(connLost) }) }
	closeDestroyed := func() { destroyedOnce.Do(func() { close(sessionDestroyed) }) }

	// Keepalive: send periodic pings to detect dead connections
	// and keep WebSocket alive through NAT/firewalls.
	go func() {
		ticker := time.NewTicker(cliPingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-connCtx.Done():
				return
			case <-ticker.C:
				if err := ws.Send(connCtx, protocol.TypePing, nil); err != nil {
					a.Logger.Debug("keepalive ping failed", "err", err)
					closeConnLost()
					return
				}
			}
		}
	}()

	// Bridge: process stdout → local terminal + relay
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, readErr := proc.Read(buf)
			if n > 0 {
				// Echo to local terminal
				os.Stdout.Write(buf[:n])

				if sendErr := ws.Send(connCtx, protocol.TypeStdout, buf[:n]); sendErr != nil {
					a.Logger.Debug("send stdout failed", "err", sendErr)
					closeConnLost()
					return
				}
			}
			if readErr != nil {
				if readErr != io.EOF {
					a.Logger.Error("read process", "err", readErr)
				}
				closeProcDead()
				return
			}
		}
	}()

	// Secondary exit detection: Wait() catches process exit even if PTY read doesn't EOF
	go func() {
		select {
		case <-processExited:
			closeProcDead()
		case <-connCtx.Done():
		}
	}()

	// Bridge: relay → process stdin (also handles TypeRestart)
	go func() {
		for {
			mt, pl, recvErr := ws.Receive(connCtx)
			if recvErr != nil {
				a.Logger.Debug("ws receive ended", "err", recvErr)
				closeConnLost()
				return
			}
			switch mt {
			case protocol.TypeStdin:
				if a.Mode == "pty" {
					if _, writeErr := proc.Write(pl); writeErr != nil {
						a.Logger.Error("write to pty", "err", writeErr)
					}
				}
			case protocol.TypeResize:
				if a.Mode == "pty" && ptyProc != nil {
					var sz protocol.Resize
					if err := protocol.DecodeJSON(pl, &sz); err == nil {
						ptyProc.Resize(sz.Cols, sz.Rows)
					}
				}
			case protocol.TypeViewerCount:
				var vc protocol.ViewerCount
				if err := protocol.DecodeJSON(pl, &vc); err == nil {
					a.Logger.Info("viewers", "count", vc.Count)
				}
			case protocol.TypeRestart:
				select {
				case restartCh <- struct{}{}:
				default:
				}
			case protocol.TypeEnd:
				a.Logger.Info("session destroyed by owner")
				fmt.Fprintf(os.Stderr, "Session destroyed by owner. Shutting down.\r\n")
				closeDestroyed()
				return
			case protocol.TypePing:
				ws.Send(connCtx, protocol.TypePong, nil)
			}
		}
	}()

	// Wait for process death, connection loss, or session destruction (whichever comes first).
	select {
	case <-procDead:
		// Process exited — handle ProcessExited/Restart on this connection.
		exitCode := 0
		select {
		case code := <-processExited:
			exitCode = code
		default:
		}
		return a.handleProcessExited(appCtx, ws, exitCode, restartCh, connLost, sessionDestroyed)
	case <-sessionDestroyed:
		// Session was destroyed by owner — clean exit.
		return connectionResult{}
	case <-connLost:
		// WebSocket error — connection is dead.
		// Check if process also died (race between ws error and process exit).
		select {
		case <-procDead:
			exitCode := 0
			select {
			case code := <-processExited:
				exitCode = code
			default:
			}
			return connectionResult{processExited: true, exitCode: exitCode}
		default:
			return connectionResult{err: fmt.Errorf("connection lost")}
		}
	case <-appCtx.Done():
		return connectionResult{}
	}
}

// handleProcessExited sends ProcessExited to the relay and, in manual restart
// mode, waits for TypeRestart. The stdin bridge goroutine is still running and
// will signal restartCh when TypeRestart arrives, so there is only one reader
// on the websocket at a time.
func (a *App) handleProcessExited(
	appCtx context.Context,
	ws *WSConn,
	exitCode int,
	restartCh <-chan struct{},
	connLost <-chan struct{},
	sessionDestroyed <-chan struct{},
) connectionResult {
	switch a.Restart {
	case "manual":
		exitPayload := protocol.ProcessExited{ExitCode: exitCode}
		if err := ws.Send(appCtx, protocol.TypeProcessExited, exitPayload); err != nil {
			a.Logger.Warn("failed to send process exited", "err", err)
			return connectionResult{processExited: true, exitCode: exitCode}
		}
		fmt.Fprintf(os.Stderr, "Process exited (code %d). Waiting for restart...\r\n", exitCode)
		a.Logger.Info("waiting for restart signal", "exit_code", exitCode)

		// Wait for the stdin bridge to receive TypeRestart.
		select {
		case <-restartCh:
			a.Logger.Info("restart signal received")
			fmt.Fprintf(os.Stderr, "Restart signal received. Restarting process...\r\n")
			return connectionResult{processExited: true, exitCode: exitCode, restarted: true}
		case <-sessionDestroyed:
			a.Logger.Info("session destroyed while waiting for restart")
			return connectionResult{}
		case <-connLost:
			a.Logger.Info("connection lost while waiting for restart")
			return connectionResult{processExited: true, exitCode: exitCode}
		case <-appCtx.Done():
			return connectionResult{processExited: true, exitCode: exitCode}
		}
	case "auto":
		return connectionResult{processExited: true, exitCode: exitCode}
	default: // "never" or unset
		return connectionResult{processExited: true, exitCode: exitCode}
	}
}
