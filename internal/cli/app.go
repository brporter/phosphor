package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/brporter/phosphor/internal/protocol"
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

// connectionResult is the structured result from a single connection attempt.
type connectionResult struct {
	err           error
	processExited bool
	exitCode      int
	ws            *WSConn // keep WebSocket alive for manual restart wait
}

// Run starts the CLI session with restart support.
func (a *App) Run(ctx context.Context) error {
	appCtx, appCancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer appCancel()

	for {
		err := a.runWithProcess(appCtx)
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
			fmt.Fprintf(os.Stderr, "Process exited. Restarting...\n")
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
func (a *App) runWithProcess(appCtx context.Context) error {
	var cols, rows int
	var proc io.ReadWriteCloser
	var ptyProc PTYProcess

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
	} else {
		proc = NewPipeReader(os.Stdin)
		cols = 80
		rows = 24
	}

	var sessionID, reconnectToken string
	attempt := 0

	for {
		result := a.runConnection(appCtx, proc, ptyProc, cols, rows, &sessionID, &reconnectToken)

		if result.processExited {
			switch a.Restart {
			case "manual":
				if result.ws != nil {
					// Send ProcessExited to relay, wait for TypeRestart
					exitPayload := protocol.ProcessExited{ExitCode: result.exitCode}
					if err := result.ws.Send(appCtx, protocol.TypeProcessExited, exitPayload); err != nil {
						return fmt.Errorf("send process exited: %w", err)
					}
					fmt.Fprintf(os.Stderr, "Process exited (code %d). Waiting for restart...\n", result.exitCode)
					a.Logger.Info("waiting for restart signal", "exit_code", result.exitCode)

					// Wait loop for TypeRestart
					for {
						mt, _, recvErr := result.ws.Receive(appCtx)
						if recvErr != nil {
							a.Logger.Info("connection lost while waiting for restart", "err", recvErr)
							return fmt.Errorf("connection lost while waiting for restart")
						}
						if mt == protocol.TypeRestart {
							a.Logger.Info("restart signal received")
							fmt.Fprintf(os.Stderr, "Restart signal received. Restarting process...\n")
							result.ws.Close()
							return errProcessExited // triggers outer loop in Run()
						}
						if mt == protocol.TypePing {
							result.ws.Send(appCtx, protocol.TypePong, nil)
						}
					}
				}
				// No ws available, just end
				return nil
			case "auto":
				if result.ws != nil {
					result.ws.Close()
				}
				return errProcessExited
			default: // "never" or unset
				if result.ws != nil {
					result.ws.Close()
				}
				return nil
			}
		}

		if result.err == nil || appCtx.Err() != nil {
			return nil
		}

		// Connection lost — retry with backoff
		delay := backoffSchedule[min(attempt, len(backoffSchedule)-1)]
		attempt++
		fmt.Fprintf(os.Stderr, "Relay disconnected. Reconnecting in %s...\n", delay)
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
func (a *App) runConnection(
	appCtx context.Context,
	proc io.ReadWriteCloser,
	ptyProc PTYProcess,
	cols, rows int,
	sessionID, reconnectToken *string,
) connectionResult {
	a.Logger.Info("connecting to relay", "url", a.Config.RelayURL)

	wsURL := a.Config.RelayURL + "/ws/cli"
	ws, err := ConnectWebSocket(appCtx, wsURL)
	if err != nil {
		return connectionResult{err: fmt.Errorf("connect to relay: %w", err)}
	}

	// Send Hello
	hello := protocol.Hello{
		Token:          a.Token,
		Mode:           a.Mode,
		Cols:           cols,
		Rows:           rows,
		Command:        strings.Join(a.Command, " "),
		SessionID:      *sessionID,
		ReconnectToken: *reconnectToken,
	}
	if err := ws.Send(appCtx, protocol.TypeHello, hello); err != nil {
		ws.Close()
		return connectionResult{err: fmt.Errorf("send hello: %w", err)}
	}

	// Read Welcome
	msgType, payload, err := ws.Receive(appCtx)
	if err != nil {
		ws.Close()
		return connectionResult{err: fmt.Errorf("receive welcome: %w", err)}
	}
	if msgType == protocol.TypeError {
		var e protocol.Error
		protocol.DecodeJSON(payload, &e)
		ws.Close()
		return connectionResult{err: fmt.Errorf("server error: %s: %s", e.Code, e.Message)}
	}
	if msgType != protocol.TypeWelcome {
		ws.Close()
		return connectionResult{err: fmt.Errorf("unexpected message type: 0x%02x", msgType)}
	}
	var welcome protocol.Welcome
	if err := protocol.DecodeJSON(payload, &welcome); err != nil {
		ws.Close()
		return connectionResult{err: fmt.Errorf("decode welcome: %w", err)}
	}

	*sessionID = welcome.SessionID
	*reconnectToken = welcome.ReconnectToken

	if hello.SessionID == "" {
		fmt.Fprintf(os.Stderr, "Session live: %s\n", welcome.ViewURL)
	} else {
		fmt.Fprintf(os.Stderr, "Reconnected: %s\n", welcome.ViewURL)
	}
	a.Logger.Info("session active", "session_id", welcome.SessionID, "url", welcome.ViewURL)

	// connCtx scoped to this single connection
	connCtx, connCancel := context.WithCancel(appCtx)
	defer connCancel()

	procDead := make(chan struct{})

	// Bridge: process stdout → relay
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, readErr := proc.Read(buf)
			if n > 0 {
				if sendErr := ws.Send(connCtx, protocol.TypeStdout, buf[:n]); sendErr != nil {
					a.Logger.Debug("send stdout failed", "err", sendErr)
					connCancel()
					return
				}
			}
			if readErr != nil {
				if readErr != io.EOF {
					a.Logger.Error("read process", "err", readErr)
				}
				close(procDead)
				connCancel()
				return
			}
		}
	}()

	// Bridge: relay → process stdin
	go func() {
		for {
			mt, pl, recvErr := ws.Receive(connCtx)
			if recvErr != nil {
				a.Logger.Debug("ws receive ended", "err", recvErr)
				connCancel()
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
			case protocol.TypePing:
				ws.Send(connCtx, protocol.TypePong, nil)
			}
		}
	}()

	<-connCtx.Done()

	select {
	case <-procDead:
		return connectionResult{processExited: true, exitCode: 0, ws: ws}
	default:
		ws.Close()
		return connectionResult{err: fmt.Errorf("connection lost")}
	}
}
