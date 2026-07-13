//go:build js && wasm

package webssh

import (
	"errors"
	"fmt"
	"io"
	"net"
	"syscall/js"

	"golang.org/x/crypto/ssh"
)

// connectOptions is the JS-supplied config for a connection.
type connectOptions struct {
	wsURL      string
	token      string
	username   string
	privateKey string // PEM, optional
	keyPass    string // passphrase for privateKey, optional
	callbacks  js.Value
	rows, cols int
}

// session wraps a live SSH session and exposes write/resize/disconnect to JS.
type session struct {
	client  *ssh.Client
	sess    *ssh.Session
	stdin   io.WriteCloser
	wsConn  *wsConn
	onData  js.Value
	onClose js.Value
}

// connect performs the SSH handshake against the host through the relay
// tunnel and starts an interactive shell. It returns a JS handle object.
func connect(opts connectOptions) (js.Value, error) {
	ws := js.Global().Get("WebSocket").New(opts.wsURL, "phosphor-ssh")
	ws.Set("binaryType", "arraybuffer")

	// Wait for the socket to open, then send the auth prelude and wait for
	// the {"ok":true} ack before handing the raw socket to the SSH client.
	if err := openAndAuth(ws, opts.token); err != nil {
		ws.Call("close")
		return js.Undefined(), err
	}

	wsc := newWSConn(ws)
	cb := opts.callbacks

	config := &ssh.ClientConfig{
		User:            opts.username,
		HostKeyCallback: jsHostKeyCallback(cb),
		Auth:            authMethods(opts, cb),
	}

	sshConn, chans, reqs, err := ssh.NewClientConn(wsc, opts.username, config)
	if err != nil {
		wsc.Close()
		return js.Undefined(), fmt.Errorf("ssh handshake: %w", err)
	}
	client := ssh.NewClient(sshConn, chans, reqs)

	sess, err := client.NewSession()
	if err != nil {
		client.Close()
		return js.Undefined(), fmt.Errorf("opening session: %w", err)
	}

	modes := ssh.TerminalModes{ssh.ECHO: 1, ssh.TTY_OP_ISPEED: 14400, ssh.TTY_OP_OSPEED: 14400}
	rows, cols := opts.rows, opts.cols
	if rows == 0 {
		rows = 24
	}
	if cols == 0 {
		cols = 80
	}
	if err := sess.RequestPty("xterm-256color", rows, cols, modes); err != nil {
		client.Close()
		return js.Undefined(), fmt.Errorf("requesting pty: %w", err)
	}

	stdin, err := sess.StdinPipe()
	if err != nil {
		client.Close()
		return js.Undefined(), err
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		client.Close()
		return js.Undefined(), err
	}
	stderr, err := sess.StderrPipe()
	if err != nil {
		client.Close()
		return js.Undefined(), err
	}

	if err := sess.Shell(); err != nil {
		client.Close()
		return js.Undefined(), fmt.Errorf("starting shell: %w", err)
	}

	s := &session{
		client:  client,
		sess:    sess,
		stdin:   stdin,
		wsConn:  wsc,
		onData:  cb.Get("onData"),
		onClose: cb.Get("onClose"),
	}
	go s.pump(stdout)
	go s.pump(stderr)
	go func() {
		sess.Wait()
		s.close()
	}()

	return s.handle(), nil
}

// pump forwards process output to the JS onData callback as a Uint8Array.
func (s *session) pump(r io.Reader) {
	buf := make([]byte, 32<<10)
	for {
		n, err := r.Read(buf)
		if n > 0 && s.onData.Type() == js.TypeFunction {
			arr := js.Global().Get("Uint8Array").New(n)
			js.CopyBytesToJS(arr, buf[:n])
			s.onData.Invoke(arr)
		}
		if err != nil {
			return
		}
	}
}

func (s *session) close() {
	s.sess.Close()
	s.client.Close()
	s.wsConn.Close()
	if s.onClose.Type() == js.TypeFunction {
		s.onClose.Invoke()
	}
}

// handle builds the JS object returned to the caller.
func (s *session) handle() js.Value {
	obj := js.Global().Get("Object").New()
	obj.Set("write", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		var data []byte
		if args[0].Type() == js.TypeString {
			data = []byte(args[0].String())
		} else {
			n := args[0].Get("length").Int()
			data = make([]byte, n)
			js.CopyBytesToGo(data, args[0])
		}
		go s.stdin.Write(data)
		return nil
	}))
	obj.Set("resize", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 2 {
			return nil
		}
		cols, rows := args[0].Int(), args[1].Int()
		go s.sess.WindowChange(rows, cols)
		return nil
	}))
	obj.Set("disconnect", js.FuncOf(func(this js.Value, args []js.Value) any {
		go s.close()
		return nil
	}))
	return obj
}

// authMethods builds the ordered auth method list. A supplied private key is
// tried first; password and keyboard-interactive fall back to JS prompts.
func authMethods(opts connectOptions, cb js.Value) []ssh.AuthMethod {
	var methods []ssh.AuthMethod
	if opts.privateKey != "" {
		var signer ssh.Signer
		var err error
		if opts.keyPass != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(opts.privateKey), []byte(opts.keyPass))
		} else {
			signer, err = ssh.ParsePrivateKey([]byte(opts.privateKey))
		}
		if err == nil {
			methods = append(methods, ssh.PublicKeys(signer))
		}
	}
	if fn := cb.Get("onPassword"); fn.Type() == js.TypeFunction {
		methods = append(methods, ssh.PasswordCallback(func() (string, error) {
			v, err := await(fn.Invoke())
			if err != nil {
				return "", err
			}
			return v.String(), nil
		}))
	}
	if fn := cb.Get("onKeyboardInteractive"); fn.Type() == js.TypeFunction {
		methods = append(methods, ssh.KeyboardInteractive(func(name, instruction string, questions []string, echos []bool) ([]string, error) {
			qArr := js.Global().Get("Array").New()
			for _, q := range questions {
				qArr.Call("push", q)
			}
			v, err := await(fn.Invoke(name, instruction, qArr))
			if err != nil {
				return nil, err
			}
			answers := make([]string, len(questions))
			for i := range answers {
				if i < v.Length() {
					answers[i] = v.Index(i).String()
				}
			}
			return answers, nil
		}))
	}
	return methods
}

// jsHostKeyCallback asks JS to verify (TOFU / pin check) the host key.
func jsHostKeyCallback(cb js.Value) ssh.HostKeyCallback {
	fn := cb.Get("onHostKey")
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		if fn.Type() != js.TypeFunction {
			return nil // accept if the UI provides no verifier
		}
		fingerprint := ssh.FingerprintSHA256(key)
		v, err := await(fn.Invoke(fingerprint, key.Type()))
		if err != nil {
			return err
		}
		if !v.Truthy() {
			return errors.New("host key rejected")
		}
		return nil
	}
}
