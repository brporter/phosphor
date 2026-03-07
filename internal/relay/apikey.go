package relay

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	gonanoid "github.com/matoous/go-nanoid/v2"
)

// APIKeyClaims holds the verified claims extracted from an API key JWT.
type APIKeyClaims struct {
	Subject string
	KeyID   string
	Issuer  string
}

// apiKeyClaims is the internal JWT claims structure.
type apiKeyClaims struct {
	Subject  string           `json:"sub"`
	Issuer   string           `json:"iss"`
	KeyID    string           `json:"jti"`
	IssuedAt *jwt.NumericDate `json:"iat"`
}

// GenerateAPIKey creates an HS256-signed JWT API key.
// It returns the raw JWT string (without any prefix), the key ID, and an error.
func GenerateAPIKey(secret []byte, provider, sub string) (string, string, error) {
	if len(secret) == 0 {
		return "", "", errors.New("secret must not be empty")
	}
	if provider == "" || sub == "" {
		return "", "", errors.New("provider and sub must not be empty")
	}

	keyID, err := gonanoid.New(12)
	if err != nil {
		return "", "", fmt.Errorf("generating key ID: %w", err)
	}

	sig, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.HS256, Key: secret},
		(&jose.SignerOptions{}).WithType("JWT"),
	)
	if err != nil {
		return "", "", fmt.Errorf("creating signer: %w", err)
	}

	claims := apiKeyClaims{
		Subject:  provider + ":" + sub,
		Issuer:   "phosphor",
		KeyID:    keyID,
		IssuedAt: jwt.NewNumericDate(time.Now()),
	}

	raw, err := jwt.Signed(sig).Claims(claims).Serialize()
	if err != nil {
		return "", "", fmt.Errorf("signing token: %w", err)
	}

	return raw, keyID, nil
}

// VerifyAPIKey parses and verifies an HS256-signed JWT API key.
// It checks that the issuer is "phosphor" and returns the claims.
func VerifyAPIKey(secret []byte, token string) (*APIKeyClaims, error) {
	tok, err := jwt.ParseSigned(token, []jose.SignatureAlgorithm{jose.HS256})
	if err != nil {
		return nil, fmt.Errorf("parsing token: %w", err)
	}

	var claims apiKeyClaims
	if err := tok.Claims(secret, &claims); err != nil {
		return nil, fmt.Errorf("verifying token: %w", err)
	}

	if claims.Issuer != "phosphor" {
		return nil, errors.New("invalid issuer")
	}

	return &APIKeyClaims{
		Subject: claims.Subject,
		KeyID:   claims.KeyID,
		Issuer:  claims.Issuer,
	}, nil
}

// ParseAPIKeySubject splits a "provider:sub" subject string into its parts.
func ParseAPIKeySubject(subject string) (string, string, error) {
	parts := strings.SplitN(subject, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid subject format: %q", subject)
	}
	return parts[0], parts[1], nil
}
