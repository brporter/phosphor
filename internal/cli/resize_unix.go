//go:build !windows

package cli

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/term"
)

// watchTerminalResize listens for SIGWINCH and resizes the PTY to match the local terminal.
func watchTerminalResize(ctx context.Context, ptyProc PTYProcess) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	defer signal.Stop(ch)

	for {
		select {
		case <-ch:
			cols, rows, err := term.GetSize(int(os.Stdin.Fd()))
			if err == nil {
				ptyProc.Resize(cols, rows)
			}
		case <-ctx.Done():
			return
		}
	}
}
