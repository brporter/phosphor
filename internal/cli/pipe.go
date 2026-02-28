package cli

import "io"

// pipeReader wraps stdin as a ReadWriteCloser for pipe mode.
// Writes are discarded (pipe mode is view-only).
type pipeReader struct {
	r io.Reader
}

// NewPipeReader creates a pipe-mode I/O wrapper around stdin.
func NewPipeReader(r io.Reader) io.ReadWriteCloser {
	return &pipeReader{r: r}
}

func (p *pipeReader) Read(buf []byte) (int, error) {
	return p.r.Read(buf)
}

func (p *pipeReader) Write(buf []byte) (int, error) {
	// Discard stdin from viewers in pipe mode
	return len(buf), nil
}

func (p *pipeReader) Close() error {
	if c, ok := p.r.(io.Closer); ok {
		return c.Close()
	}
	return nil
}
