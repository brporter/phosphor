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

// Run starts the CLI session with automatic reconnection.
func (a *App) Run(ctx context.Context) error {
	appCtx, appCancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer appCancel()

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
		err := a.runConnection(appCtx, proc, ptyProc, cols, rows, &sessionID, &reconnectToken)
		if errors.Is(err, errProcessExited) {
			a.Logger.Info("session ended")
			return nil
		}
		if appCtx.Err() != nil {
			a.Logger.Info("session ended")
			return nil
		}

		// Pick backoff delay
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
) error {
	a.Logger.Info("connecting to relay", "url", a.Config.RelayURL)

	wsURL := a.Config.RelayURL + "/ws/cli"
	ws, err := ConnectWebSocket(appCtx, wsURL)
	if err != nil {
		return fmt.Errorf("connect to relay: %w", err)
	}
	defer ws.Close()

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
		return fmt.Errorf("send hello: %w", err)
	}

	// Read Welcome
	msgType, payload, err := ws.Receive(appCtx)
	if err != nil {
		return fmt.Errorf("receive welcome: %w", err)
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

	// Check if the process itself died
	select {
	case <-procDead:
		return errProcessExited
	default:
		return fmt.Errorf("connection lost")
	}
}
