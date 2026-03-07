//go:build !windows

package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"syscall"

	"github.com/creack/pty"
)

// StartPTYAsUser spawns a PTY running the given shell as the specified local user.
func StartPTYAsUser(shell string, localUser string) (PTYProcess, int, int, error) {
	u, err := user.Lookup(localUser)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("lookup user %q: %w", localUser, err)
	}
	uid, _ := strconv.ParseUint(u.Uid, 10, 32)
	gid, _ := strconv.ParseUint(u.Gid, 10, 32)

	cmd := exec.Command(shell)
	cmd.Env = []string{
		"HOME=" + u.HomeDir,
		"USER=" + u.Username,
		"SHELL=" + shell,
		"TERM=xterm-256color",
		"PATH=/usr/local/bin:/usr/bin:/bin",
	}
	cmd.Dir = u.HomeDir
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{
			Uid: uint32(uid),
			Gid: uint32(gid),
		},
		Setsid: true,
	}

	cols, rows := 80, 24
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{
		Cols: uint16(cols),
		Rows: uint16(rows),
	})
	if err != nil {
		return nil, 0, 0, fmt.Errorf("start pty as %q: %w", localUser, err)
	}

	return &daemonPTY{f: ptmx, cmd: cmd}, cols, rows, nil
}

type daemonPTY struct {
	f   *os.File
	cmd *exec.Cmd
}

func (p *daemonPTY) Read(buf []byte) (int, error)  { return p.f.Read(buf) }
func (p *daemonPTY) Write(buf []byte) (int, error) { return p.f.Write(buf) }
func (p *daemonPTY) Close() error                  { return p.f.Close() }
func (p *daemonPTY) Pid() int                      { return p.cmd.Process.Pid }

func (p *daemonPTY) Resize(cols, rows int) error {
	return pty.Setsize(p.f, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

func (p *daemonPTY) Wait(ctx context.Context) (int, error) {
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
