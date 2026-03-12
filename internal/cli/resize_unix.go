//go:build !windows

package cli

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/term"
)

// watchTerminalResize listens for SIGWINCH, resizes the PTY to match the local
// terminal, and notifies the relay of the new dimensions.
func watchTerminalResize(ctx context.Context, ptyProc PTYProcess, notifier *wsNotifier) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	defer signal.Stop(ch)

	for {
		select {
		case <-ch:
			cols, rows, err := term.GetSize(int(os.Stdin.Fd()))
			if err == nil {
				ptyProc.Resize(cols, rows)
				notifier.SendResize(cols, rows)
			}
		case <-ctx.Done():
			return
		}
	}
}
