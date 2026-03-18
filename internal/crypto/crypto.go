package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"fmt"

	"golang.org/x/crypto/pbkdf2"
)

const (
	saltSize   = 16
	keySize    = 32 // AES-256
	nonceSize  = 12 // GCM standard
	iterations = 100000
)

// GenerateSalt returns 16 cryptographically random bytes for use as a PBKDF2 salt.
func GenerateSalt() ([]byte, error) {
	salt := make([]byte, saltSize)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("generate salt: %w", err)
	}
	return salt, nil
}

// DeriveKey derives a 256-bit AES key from a passphrase and salt using PBKDF2-SHA256.
func DeriveKey(passphrase string, salt []byte) ([]byte, error) {
	if len(salt) == 0 {
		return nil, fmt.Errorf("salt must not be empty")
	}
	key := pbkdf2.Key([]byte(passphrase), salt, iterations, keySize, sha256.New)
	return key, nil
}

// Encrypt encrypts plaintext using AES-256-GCM.
// Returns [12-byte nonce][ciphertext + 16-byte GCM tag].
func Encrypt(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}

	nonce := make([]byte, nonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)
	// Prepend nonce to ciphertext
	result := make([]byte, nonceSize+len(ciphertext))
	copy(result, nonce)
	copy(result[nonceSize:], ciphertext)
	return result, nil
}

// Decrypt decrypts data produced by Encrypt using AES-256-GCM.
// Expects [12-byte nonce][ciphertext + 16-byte GCM tag].
func Decrypt(key, data []byte) ([]byte, error) {
	if len(data) < nonceSize+16 {
		return nil, fmt.Errorf("ciphertext too short")
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}

	nonce := data[:nonceSize]
	ciphertext := data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}
	return plaintext, nil
}
