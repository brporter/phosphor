package cli

import (
	"io"
	"strings"
	"testing"
)

func TestPipeReader_Read(t *testing.T) {
	r := strings.NewReader("hello")
	p := NewPipeReader(r)

	buf := make([]byte, 10)
	n, err := p.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := string(buf[:n]); got != "hello" {
		t.Errorf("expected %q, got %q", "hello", got)
	}
}

func TestPipeReader_Write(t *testing.T) {
	r := strings.NewReader("")
	p := NewPipeReader(r)

	data := []byte("data")
	n, err := p.Write(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 4 {
		t.Errorf("expected n=4, got n=%d", n)
	}
}

func TestPipeReader_Close_WithCloser(t *testing.T) {
	r := io.NopCloser(strings.NewReader(""))
	p := NewPipeReader(r)

	if err := p.Close(); err != nil {
		t.Errorf("expected no error closing with a Closer, got: %v", err)
	}
}

func TestPipeReader_Close_WithoutCloser(t *testing.T) {
	r := strings.NewReader("")
	p := NewPipeReader(r)

	if err := p.Close(); err != nil {
		t.Errorf("expected no error closing without a Closer, got: %v", err)
	}
}
