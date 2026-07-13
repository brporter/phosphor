//go:build js && wasm

package webssh

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"strings"
	"syscall/js"

	"golang.org/x/crypto/ssh"
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

// jsGenerateKeypair creates an ed25519 keypair. Optional first arg is a
// passphrase to encrypt the private key PEM.
func jsGenerateKeypair(this js.Value, args []js.Value) any {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return jsError(err.Error())
	}

	var block *pem.Block
	if len(args) > 0 && args[0].Type() == js.TypeString && args[0].String() != "" {
		block, err = ssh.MarshalPrivateKeyWithPassphrase(priv, "phosphor browser key", []byte(args[0].String()))
	} else {
		block, err = ssh.MarshalPrivateKey(priv, "phosphor browser key")
	}
	if err != nil {
		return jsError(err.Error())
	}

	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return jsError(err.Error())
	}

	out := js.Global().Get("Object").New()
	out.Set("privateKeyPem", string(pem.EncodeToMemory(block)))
	out.Set("authorizedKey", strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub))))
	out.Set("fingerprint", ssh.FingerprintSHA256(sshPub))
	return out
}

// jsPublicKeyFromPem derives the authorized_keys line + fingerprint from a
// private key PEM (validating an optional passphrase).
func jsPublicKeyFromPem(this js.Value, args []js.Value) any {
	if len(args) == 0 || args[0].Type() != js.TypeString {
		return jsError("privateKeyPem is required")
	}
	pemStr := args[0].String()
	var signer ssh.Signer
	var err error
	if len(args) > 1 && args[1].Type() == js.TypeString && args[1].String() != "" {
		signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(pemStr), []byte(args[1].String()))
	} else {
		signer, err = ssh.ParsePrivateKey([]byte(pemStr))
	}
	if err != nil {
		return jsError(err.Error())
	}
	out := js.Global().Get("Object").New()
	out.Set("authorizedKey", strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey()))))
	out.Set("fingerprint", ssh.FingerprintSHA256(signer.PublicKey()))
	return out
}

func jsError(msg string) js.Value {
	return js.Global().Get("Error").New(msg)
}

func rejectedPromise(msg string) js.Value {
	return js.Global().Get("Promise").Call("reject", jsError(msg))
}
