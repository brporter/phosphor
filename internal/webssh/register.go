//go:build js && wasm

package webssh

import (
	"syscall/js"

	"github.com/brporter/phosphor/internal/sshkeys"
)

// Register installs the phosphorSSH global.
func Register() {
	api := js.Global().Get("Object").New()
	api.Set("connect", js.FuncOf(jsConnect))
	api.Set("generateKeypair", js.FuncOf(jsGenerateKeypair))
	api.Set("publicKeyFromPem", js.FuncOf(jsPublicKeyFromPem))
	js.Global().Set("phosphorSSH", api)
}

func optString(o js.Value, key string) string {
	v := o.Get(key)
	if v.Type() == js.TypeString {
		return v.String()
	}
	return ""
}

func optInt(o js.Value, key string) int {
	v := o.Get(key)
	if v.Type() == js.TypeNumber {
		return v.Int()
	}
	return 0
}

// jsConnect returns a Promise resolving to a session handle. Running connect
// on a goroutine keeps the JS thread free for the auth/host-key callbacks it
// makes back into JS.
func jsConnect(this js.Value, args []js.Value) any {
	if len(args) == 0 {
		return rejectedPromise("connect requires an options object")
	}
	o := args[0]
	opts := connectOptions{
		wsURL:      optString(o, "wsURL"),
		token:      optString(o, "token"),
		username:   optString(o, "username"),
		privateKey: optString(o, "privateKey"),
		keyPass:    optString(o, "keyPassphrase"),
		callbacks:  o.Get("callbacks"),
		rows:       optInt(o, "rows"),
		cols:       optInt(o, "cols"),
	}
	if opts.callbacks.Type() != js.TypeObject {
		opts.callbacks = js.Global().Get("Object").New()
	}

	handler := js.FuncOf(func(this js.Value, pargs []js.Value) any {
		resolve, reject := pargs[0], pargs[1]
		go func() {
			h, err := connect(opts)
			if err != nil {
				reject.Invoke(jsError(err.Error()))
				return
			}
			resolve.Invoke(h)
		}()
		return nil
	})
	return js.Global().Get("Promise").New(handler)
}

// Synchronous API results use an {ok, ...} envelope rather than throwing:
// js.FuncOf callbacks cannot raise a JS exception (a Go panic kills the whole
// module), so the wasm.ts wrapper checks `ok` and rethrows on the JS side.
func errResult(msg string) js.Value {
	out := js.Global().Get("Object").New()
	out.Set("ok", false)
	out.Set("error", msg)
	return out
}

func okResult() js.Value {
	out := js.Global().Get("Object").New()
	out.Set("ok", true)
	return out
}

// jsGenerateKeypair creates an ed25519 keypair. Optional first arg is a
// passphrase to encrypt the private key PEM.
func jsGenerateKeypair(this js.Value, args []js.Value) any {
	passphrase := ""
	if len(args) > 0 && args[0].Type() == js.TypeString {
		passphrase = args[0].String()
	}
	kp, err := sshkeys.GenerateKeypair(passphrase)
	if err != nil {
		return errResult(err.Error())
	}
	out := okResult()
	out.Set("privateKeyPem", kp.PrivateKeyPem)
	out.Set("authorizedKey", kp.AuthorizedKey)
	out.Set("fingerprint", kp.Fingerprint)
	return out
}

// jsPublicKeyFromPem derives the authorized_keys line + fingerprint from a
// private key PEM (validating an optional passphrase).
func jsPublicKeyFromPem(this js.Value, args []js.Value) any {
	if len(args) == 0 || args[0].Type() != js.TypeString {
		return errResult("privateKeyPem is required")
	}
	passphrase := ""
	if len(args) > 1 && args[1].Type() == js.TypeString {
		passphrase = args[1].String()
	}
	info, err := sshkeys.PublicKeyFromPem(args[0].String(), passphrase)
	if err != nil {
		return errResult(err.Error())
	}
	out := okResult()
	out.Set("authorizedKey", info.AuthorizedKey)
	out.Set("fingerprint", info.Fingerprint)
	return out
}

func jsError(msg string) js.Value {
	return js.Global().Get("Error").New(msg)
}

func rejectedPromise(msg string) js.Value {
	return js.Global().Get("Promise").Call("reject", jsError(msg))
}
