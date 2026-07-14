// Package sshkeys generates browser SSH keypairs and derives public-key info
// from private key PEMs. It is deliberately free of build tags so the logic
// behind the wasm bindings in internal/webssh can be tested with plain go test.
package sshkeys

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"strings"

	"golang.org/x/crypto/ssh"
)

// comment embedded in generated private keys and authorized_keys lines.
const keyComment = "phosphor browser key"

// Keypair is a freshly generated ed25519 key in the formats the web UI needs.
type Keypair struct {
	PrivateKeyPem string
	AuthorizedKey string
	Fingerprint   string
}

// PublicKeyInfo is the public half derived from an imported private key.
type PublicKeyInfo struct {
	AuthorizedKey string
	Fingerprint   string
}

// GenerateKeypair creates an ed25519 keypair. A non-empty passphrase encrypts
// the private key PEM (OpenSSH format, bcrypt KDF).
func GenerateKeypair(passphrase string) (Keypair, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return Keypair{}, err
	}

	var block *pem.Block
	if passphrase != "" {
		block, err = ssh.MarshalPrivateKeyWithPassphrase(priv, keyComment, []byte(passphrase))
	} else {
		block, err = ssh.MarshalPrivateKey(priv, keyComment)
	}
	if err != nil {
		return Keypair{}, err
	}

	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return Keypair{}, err
	}

	return Keypair{
		PrivateKeyPem: string(pem.EncodeToMemory(block)),
		AuthorizedKey: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub))),
		Fingerprint:   ssh.FingerprintSHA256(sshPub),
	}, nil
}

// PublicKeyFromPem derives the authorized_keys line and fingerprint from a
// private key PEM, validating the passphrase if one is supplied.
func PublicKeyFromPem(pemStr, passphrase string) (PublicKeyInfo, error) {
	var signer ssh.Signer
	var err error
	if passphrase != "" {
		signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(pemStr), []byte(passphrase))
	} else {
		signer, err = ssh.ParsePrivateKey([]byte(pemStr))
	}
	if err != nil {
		return PublicKeyInfo{}, err
	}
	return PublicKeyInfo{
		AuthorizedKey: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey()))),
		Fingerprint:   ssh.FingerprintSHA256(signer.PublicKey()),
	}, nil
}
