//go:build windows

package cli

import (
	"fmt"
	"os"
	"strings"
	"github.com/UserExistsError/conpty"
	"golang.org/x/sys/windows"
)

type windowsPTY struct {
	cpty *conpty.ConPty
}

func (p *windowsPTY) Read(buf []byte) (int, error)  { return p.cpty.Read(buf) }
func (p *windowsPTY) Write(buf []byte) (int, error) { return p.cpty.Write(buf) }
func (p *windowsPTY) Close() error                  { return p.cpty.Close() }

func (p *windowsPTY) Resize(cols, rows int) error {
	return p.cpty.Resize(cols, rows)
}

// getConsoleSize returns the current console width and height.
func getConsoleSize() (int, int, error) {
	h, err := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE)
	if err != nil {
		return 0, 0, err
	}
	var info windows.ConsoleScreenBufferInfo
	err = windows.GetConsoleScreenBufferInfo(h, &info)
	if err != nil {
		return 0, 0, err
	}
	w := int(info.Window.Right-info.Window.Left) + 1
	ht := int(info.Window.Bottom-info.Window.Top) + 1
	return w, ht, nil
}

// StartPTY spawns a command in a ConPTY and returns the PTY process, cols, and rows.
func StartPTY(command []string) (PTYProcess, int, int, error) {
	cols, rows := 80, 24

	if w, h, err := getConsoleSize(); err == nil && w > 0 && h > 0 {
		cols = w
		rows = h
	}

	cmdLine := strings.Join(command, " ")

	opts := []conpty.ConPtyOption{
		conpty.ConPtyDimensions(cols, rows),
		conpty.ConPtyEnv(os.Environ()),
	}

	cpty, err := conpty.Start(cmdLine, opts...)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("conpty start: %w", err)
	}

	return &windowsPTY{cpty: cpty}, cols, rows, nil
}
