package sshgate

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"os"

	"golang.org/x/crypto/ssh"
)

// LoadOrCreateHostKey reads the SSH host key from path, generating and
// persisting a new ed25519 key if the file does not exist. The key must be
// stable across restarts so CLI host-key pinning keeps working; in
// production the path lives on a persistent volume.
func LoadOrCreateHostKey(path string) (ssh.Signer, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		signer, err := ssh.ParsePrivateKey(data)
		if err != nil {
			return nil, fmt.Errorf("parsing host key %s: %w", path, err)
		}
		return signer, nil
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("reading host key %s: %w", path, err)
	}

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generating host key: %w", err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "phosphor host key")
	if err != nil {
		return nil, fmt.Errorf("marshaling host key: %w", err)
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0600); err != nil {
		return nil, fmt.Errorf("persisting host key %s: %w", path, err)
	}
	return ssh.NewSignerFromKey(priv)
}
