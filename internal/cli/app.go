package cli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

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

// Run starts the CLI session.
func (a *App) Run(ctx context.Context) error {
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()

	a.Logger.Info("connecting to relay", "url", a.Config.RelayURL)

	wsURL := a.Config.RelayURL + "/ws/cli"
	ws, err := ConnectWebSocket(ctx, wsURL)
	if err != nil {
		return fmt.Errorf("connect to relay: %w", err)
	}
	defer ws.Close()

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

	// Send Hello
	hello := protocol.Hello{
		Token:   a.Token,
		Mode:    a.Mode,
		Cols:    cols,
		Rows:    rows,
		Command: strings.Join(a.Command, " "),
	}
	if err := ws.Send(ctx, protocol.TypeHello, hello); err != nil {
		return fmt.Errorf("send hello: %w", err)
	}

	// Read Welcome
	msgType, payload, err := ws.Receive(ctx)
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

	fmt.Fprintf(os.Stderr, "Session live: %s\n", welcome.ViewURL)
	a.Logger.Info("session started", "session_id", welcome.SessionID, "url", welcome.ViewURL)

	// Bridge: process stdout → relay
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := proc.Read(buf)
			if n > 0 {
				if sendErr := ws.Send(ctx, protocol.TypeStdout, buf[:n]); sendErr != nil {
					a.Logger.Error("send stdout", "err", sendErr)
					cancel()
					return
				}
			}
			if err != nil {
				if err != io.EOF {
					a.Logger.Error("read process", "err", err)
				}
				cancel()
				return
			}
		}
	}()

	// Bridge: relay → process stdin (PTY mode only)
	go func() {
		for {
			msgType, payload, err := ws.Receive(ctx)
			if err != nil {
				a.Logger.Debug("ws receive ended", "err", err)
				cancel()
				return
			}
			switch msgType {
			case protocol.TypeStdin:
				if a.Mode == "pty" {
					if _, err := proc.Write(payload); err != nil {
						a.Logger.Error("write to pty", "err", err)
					}
				}
			case protocol.TypeResize:
				if a.Mode == "pty" && ptyProc != nil {
					var sz protocol.Resize
					if err := protocol.DecodeJSON(payload, &sz); err == nil {
						ptyProc.Resize(sz.Cols, sz.Rows)
					}
				}
			case protocol.TypeViewerCount:
				var vc protocol.ViewerCount
				if err := protocol.DecodeJSON(payload, &vc); err == nil {
					a.Logger.Info("viewers", "count", vc.Count)
				}
			case protocol.TypePing:
				ws.Send(ctx, protocol.TypePong, nil)
			}
		}
	}()

	<-ctx.Done()
	a.Logger.Info("session ended")
	return nil
}
