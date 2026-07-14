package sshkeys

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// ed25519Pem returns an OpenSSH-format private key PEM, encrypted when
// passphrase is non-empty.
func ed25519Pem(t *testing.T, passphrase string) string {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var block *pem.Block
	if passphrase != "" {
		block, err = ssh.MarshalPrivateKeyWithPassphrase(priv, "test", []byte(passphrase))
	} else {
		block, err = ssh.MarshalPrivateKey(priv, "test")
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(block))
}

// rsaPkcs1Pem returns a legacy PKCS#1 "RSA PRIVATE KEY" PEM.
func rsaPkcs1Pem(t *testing.T) string {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(priv),
	}))
}

func assertKeyInfo(t *testing.T, info PublicKeyInfo) {
	t.Helper()
	if !strings.HasPrefix(info.AuthorizedKey, "ssh-") {
		t.Errorf("authorized key %q does not start with ssh-", info.AuthorizedKey)
	}
	if strings.ContainsAny(info.AuthorizedKey, "\n\r") {
		t.Errorf("authorized key %q contains line breaks", info.AuthorizedKey)
	}
	if !strings.HasPrefix(info.Fingerprint, "SHA256:") {
		t.Errorf("fingerprint %q does not start with SHA256:", info.Fingerprint)
	}
}

func TestPublicKeyFromPem(t *testing.T) {
	tests := []struct {
		name       string
		pem        func(t *testing.T) string
		passphrase string
		wantErr    string // substring; empty means success expected
	}{
		{
			name: "valid unencrypted ed25519",
			pem:  func(t *testing.T) string { return ed25519Pem(t, "") },
		},
		{
			name: "valid RSA PKCS#1",
			pem:  rsaPkcs1Pem,
		},
		{
			name:       "encrypted with correct passphrase",
			pem:        func(t *testing.T) string { return ed25519Pem(t, "secret123") },
			passphrase: "secret123",
		},
		{
			name:    "encrypted with missing passphrase",
			pem:     func(t *testing.T) string { return ed25519Pem(t, "secret123") },
			wantErr: "passphrase protected",
		},
		{
			name:       "encrypted with wrong passphrase",
			pem:        func(t *testing.T) string { return ed25519Pem(t, "secret123") },
			passphrase: "nope",
			wantErr:    "incorrect",
		},
		{
			name:       "unencrypted with spurious passphrase",
			pem:        func(t *testing.T) string { return ed25519Pem(t, "") },
			passphrase: "unnecessary",
			wantErr:    "not password protected",
		},
		{
			name:    "garbage input",
			pem:     func(t *testing.T) string { return "not a key" },
			wantErr: "no key found",
		},
		{
			name:    "empty input",
			pem:     func(t *testing.T) string { return "" },
			wantErr: "no key found",
		},
		{
			name: "surrounding whitespace tolerated",
			pem:  func(t *testing.T) string { return "\n\n" + strings.TrimSpace(ed25519Pem(t, "")) + "  \n\n" },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := PublicKeyFromPem(tt.pem(t), tt.passphrase)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil (info=%+v)", tt.wantErr, info)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error %q does not contain %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			assertKeyInfo(t, info)
		})
	}
}

func TestGenerateKeypair(t *testing.T) {
	t.Run("unencrypted", func(t *testing.T) {
		kp, err := GenerateKeypair("")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(kp.PrivateKeyPem, "OPENSSH PRIVATE KEY") {
			t.Errorf("private key PEM missing OpenSSH header: %q", kp.PrivateKeyPem)
		}
		assertKeyInfo(t, PublicKeyInfo{AuthorizedKey: kp.AuthorizedKey, Fingerprint: kp.Fingerprint})

		// Round-trip: deriving from the generated PEM matches the original.
		info, err := PublicKeyFromPem(kp.PrivateKeyPem, "")
		if err != nil {
			t.Fatal(err)
		}
		if info.AuthorizedKey != kp.AuthorizedKey || info.Fingerprint != kp.Fingerprint {
			t.Errorf("round-trip mismatch: got %+v, want %s / %s", info, kp.AuthorizedKey, kp.Fingerprint)
		}
	})

	t.Run("encrypted round-trip", func(t *testing.T) {
		kp, err := GenerateKeypair("hunter2")
		if err != nil {
			t.Fatal(err)
		}
		info, err := PublicKeyFromPem(kp.PrivateKeyPem, "hunter2")
		if err != nil {
			t.Fatal(err)
		}
		if info.Fingerprint != kp.Fingerprint {
			t.Errorf("fingerprint mismatch: got %s, want %s", info.Fingerprint, kp.Fingerprint)
		}
		// And the passphrase is actually required.
		if _, err := PublicKeyFromPem(kp.PrivateKeyPem, ""); err == nil {
			t.Error("expected error deriving from encrypted PEM without passphrase")
		}
	})
}
