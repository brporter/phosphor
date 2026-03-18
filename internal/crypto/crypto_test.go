package crypto

import (
	"bytes"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	salt, err := GenerateSalt()
	if err != nil {
		t.Fatal(err)
	}

	key, err := DeriveKey("test-passphrase", salt)
	if err != nil {
		t.Fatal(err)
	}

	plaintext := []byte("hello, encrypted terminal!")
	encrypted, err := Encrypt(key, plaintext)
	if err != nil {
		t.Fatal(err)
	}

	if bytes.Equal(encrypted, plaintext) {
		t.Fatal("encrypted data should differ from plaintext")
	}

	decrypted, err := Decrypt(key, encrypted)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(decrypted, plaintext) {
		t.Fatalf("got %q, want %q", decrypted, plaintext)
	}
}

func TestWrongKeyRejection(t *testing.T) {
	salt, err := GenerateSalt()
	if err != nil {
		t.Fatal(err)
	}

	key1, _ := DeriveKey("correct-key", salt)
	key2, _ := DeriveKey("wrong-key", salt)

	plaintext := []byte("secret data")
	encrypted, err := Encrypt(key1, plaintext)
	if err != nil {
		t.Fatal(err)
	}

	_, err = Decrypt(key2, encrypted)
	if err == nil {
		t.Fatal("expected decryption to fail with wrong key")
	}
}

func TestDifferentSaltsProduceDifferentKeys(t *testing.T) {
	salt1, _ := GenerateSalt()
	salt2, _ := GenerateSalt()

	key1, _ := DeriveKey("same-passphrase", salt1)
	key2, _ := DeriveKey("same-passphrase", salt2)

	if bytes.Equal(key1, key2) {
		t.Fatal("different salts should produce different keys")
	}
}

func TestEmptyPlaintext(t *testing.T) {
	salt, _ := GenerateSalt()
	key, _ := DeriveKey("key", salt)

	encrypted, err := Encrypt(key, []byte{})
	if err != nil {
		t.Fatal(err)
	}

	decrypted, err := Decrypt(key, encrypted)
	if err != nil {
		t.Fatal(err)
	}

	if len(decrypted) != 0 {
		t.Fatalf("expected empty plaintext, got %d bytes", len(decrypted))
	}
}
