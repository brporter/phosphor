//go:build !windows

package cli

import (
	"context"
	"errors"
	"os"
	"os/exec"

	"github.com/creack/pty"
)

type unixPTY struct {
	f   *os.File
	cmd *exec.Cmd
}

func (p *unixPTY) Read(buf []byte) (int, error)  { return p.f.Read(buf) }
func (p *unixPTY) Write(buf []byte) (int, error) { return p.f.Write(buf) }
func (p *unixPTY) Close() error                  { return p.f.Close() }

func (p *unixPTY) Resize(cols, rows int) error {
	return pty.Setsize(p.f, &pty.Winsize{
		Cols: uint16(cols),
		Rows: uint16(rows),
	})
}

func (p *unixPTY) Pid() int {
	return p.cmd.Process.Pid
}

func (p *unixPTY) Wait(ctx context.Context) (int, error) {
	done := make(chan error, 1)
	go func() { done <- p.cmd.Wait() }()

	select {
	case err := <-done:
		if err == nil {
			return 0, nil
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode(), nil
		}
		return -1, err
	case <-ctx.Done():
		return -1, ctx.Err()
	}
}

// StartPTY spawns a command in a PTY and returns the PTY process, cols, and rows.
func StartPTY(command []string) (PTYProcess, int, int, error) {
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Env = os.Environ()

	cols, rows := 80, 24
	if ws, err := pty.GetsizeFull(os.Stdin); err == nil {
		cols = int(ws.Cols)
		rows = int(ws.Rows)
	}

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{
		Cols: uint16(cols),
		Rows: uint16(rows),
	})
	if err != nil {
		return nil, 0, 0, err
	}

	return &unixPTY{f: ptmx, cmd: cmd}, cols, rows, nil
}
