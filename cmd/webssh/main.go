//go:build js && wasm

// Command webssh is the browser-side SSH client, compiled to WebAssembly. It
// exposes a `phosphorSSH` global with connect/generateKeypair helpers. The
// SSH handshake runs end-to-end against the host's sshd, so the relay only
// ever pipes ciphertext.
package main

import (
	"github.com/brporter/phosphor/internal/webssh"
)

func main() {
	webssh.Register()
	// Keep the Go runtime alive so exported callbacks remain valid.
	select {}
}
