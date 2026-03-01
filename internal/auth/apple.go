package auth

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

// GenerateAppleClientSecret creates a signed JWT for use as Apple's client_secret.
func GenerateAppleClientSecret(teamID, clientID, keyID string, privateKey *ecdsa.PrivateKey) (string, error) {
	signerKey := jose.SigningKey{Algorithm: jose.ES256, Key: privateKey}
	signer, err := jose.NewSigner(signerKey, (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", keyID))
	if err != nil {
		return "", fmt.Errorf("create signer: %w", err)
	}

	now := time.Now()
	claims := jwt.Claims{
		Issuer:   teamID,
		Subject:  clientID,
		Audience: jwt.Audience{"https://appleid.apple.com"},
		IssuedAt: jwt.NewNumericDate(now),
		Expiry:   jwt.NewNumericDate(now.Add(180 * 24 * time.Hour)),
	}

	token, err := jwt.Signed(signer).Claims(claims).Serialize()
	if err != nil {
		return "", fmt.Errorf("sign token: %w", err)
	}
	return token, nil
}

// ParseP8PrivateKey parses an Apple .p8 private key file (PKCS#8 PEM).
func ParseP8PrivateKey(pemData []byte) (*ecdsa.PrivateKey, error) {
	block, rest := pem.Decode(pemData)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found")
	}
	if block.Type != "PRIVATE KEY" {
		return nil, fmt.Errorf("expected PEM type \"PRIVATE KEY\", got %q", block.Type)
	}
	if len(bytes.TrimSpace(rest)) > 0 {
		return nil, fmt.Errorf("unexpected trailing data after PEM block")
	}

	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse PKCS8 key: %w", err)
	}

	ecKey, ok := key.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("key is not ECDSA (got %T)", key)
	}
	return ecKey, nil
}
