package cli

import "io"

// PTYProcess is a pseudo-terminal that supports read, write, close, and resize.
type PTYProcess interface {
	io.ReadWriteCloser
	Resize(cols, rows int) error
}
