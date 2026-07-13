//go:build js && wasm

// Package webssh implements a browser SSH client: an ssh.Client running over
// a WebSocket to the Phosphor relay, which pipes bytes to the host's sshd.
// The SSH handshake is end-to-end between this code and the host, so the
// relay never sees plaintext.
package webssh

import (
	"errors"
	"io"
	"net"
	"sync"
	"syscall/js"
	"time"
)

// wsConn adapts a browser WebSocket to net.Conn for x/crypto/ssh. Incoming
// binary frames are pushed by the JS onmessage callback into a buffer that
// Read drains; Write forwards to ws.send.
type wsConn struct {
	ws js.Value

	mu       sync.Mutex
	cond     *sync.Cond
	buf      []byte
	closed   bool
	err      error
	onMsg    js.Func
	onClose  js.Func
	onError  js.Func
	closeOne sync.Once
}

func newWSConn(ws js.Value) *wsConn {
	c := &wsConn{ws: ws}
	c.cond = sync.NewCond(&c.mu)
	ws.Set("binaryType", "arraybuffer")

	c.onMsg = js.FuncOf(func(this js.Value, args []js.Value) any {
		data := args[0].Get("data")
		// Text frames (e.g. the {"ok":true} prelude ack) are consumed by JS
		// before handing the socket to Go; here we only expect binary.
		if data.Type() == js.TypeString {
			return nil
		}
		arr := js.Global().Get("Uint8Array").New(data)
		n := arr.Get("length").Int()
		b := make([]byte, n)
		js.CopyBytesToGo(b, arr)
		c.mu.Lock()
		c.buf = append(c.buf, b...)
		c.cond.Broadcast()
		c.mu.Unlock()
		return nil
	})
	ws.Call("addEventListener", "message", c.onMsg)

	c.onClose = js.FuncOf(func(this js.Value, args []js.Value) any {
		c.fail(io.EOF)
		return nil
	})
	ws.Call("addEventListener", "close", c.onClose)

	c.onError = js.FuncOf(func(this js.Value, args []js.Value) any {
		c.fail(errors.New("websocket error"))
		return nil
	})
	ws.Call("addEventListener", "error", c.onError)

	return c
}

func (c *wsConn) fail(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err == nil {
		c.err = err
	}
	c.closed = true
	c.cond.Broadcast()
}

func (c *wsConn) Read(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for len(c.buf) == 0 && !c.closed {
		c.cond.Wait()
	}
	if len(c.buf) > 0 {
		n := copy(p, c.buf)
		c.buf = c.buf[n:]
		return n, nil
	}
	if c.err != nil {
		return 0, c.err
	}
	return 0, io.EOF
}

func (c *wsConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return 0, io.ErrClosedPipe
	}
	arr := js.Global().Get("Uint8Array").New(len(p))
	js.CopyBytesToJS(arr, p)
	c.ws.Call("send", arr)
	return len(p), nil
}

func (c *wsConn) Close() error {
	c.closeOne.Do(func() {
		c.mu.Lock()
		c.closed = true
		c.cond.Broadcast()
		c.mu.Unlock()
		c.ws.Call("close")
		c.onMsg.Release()
		c.onClose.Release()
		c.onError.Release()
	})
	return nil
}

type wsAddr struct{}

func (wsAddr) Network() string { return "websocket" }
func (wsAddr) String() string  { return "websocket" }

func (c *wsConn) LocalAddr() net.Addr                { return wsAddr{} }
func (c *wsConn) RemoteAddr() net.Addr               { return wsAddr{} }
func (c *wsConn) SetDeadline(t time.Time) error      { return nil }
func (c *wsConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *wsConn) SetWriteDeadline(t time.Time) error { return nil }
