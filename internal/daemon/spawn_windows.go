//go:build windows

package daemon

import (
	"context"
	"fmt"

	"github.com/UserExistsError/conpty"
)

// StartPTYAsUser spawns a ConPTY running the given shell.
// On Windows, the service runs as SYSTEM. Full CreateProcessAsUser
// integration is a future enhancement; for now we start the shell
// in the service's session.
func StartPTYAsUser(shell string, localUser string) (PTYProcess, int, int, error) {
	cols, rows := 80, 24
	opts := []conpty.ConPtyOption{
		conpty.ConPtyDimensions(cols, rows),
	}

	cpty, err := conpty.Start(shell, opts...)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("conpty start: %w", err)
	}

	return &daemonPTY{cpty: cpty}, cols, rows, nil
}

type daemonPTY struct {
	cpty *conpty.ConPty
}

func (p *daemonPTY) Read(buf []byte) (int, error)  { return p.cpty.Read(buf) }
func (p *daemonPTY) Write(buf []byte) (int, error) { return p.cpty.Write(buf) }
func (p *daemonPTY) Close() error                  { return p.cpty.Close() }
func (p *daemonPTY) Pid() int                      { return p.cpty.Pid() }

func (p *daemonPTY) Resize(cols, rows int) error {
	return p.cpty.Resize(cols, rows)
}

func (p *daemonPTY) Wait(ctx context.Context) (int, error) {
	code, err := p.cpty.Wait(ctx)
	return int(code), err
}
