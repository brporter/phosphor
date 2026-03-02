package cli

import (
	"context"
	"io"
)

// PTYProcess is a pseudo-terminal that supports read, write, close, and resize.
type PTYProcess interface {
	io.ReadWriteCloser
	Resize(cols, rows int) error
	Wait(ctx context.Context) (int, error) // blocks until process exits, returns exit code
	Pid() int                              // returns subprocess PID
}
