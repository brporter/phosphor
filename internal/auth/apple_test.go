package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

func TestGenerateAppleClientSecret(t *testing.T) {
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	secret, err := GenerateAppleClientSecret("TEAMID123", "com.example.app", "KEYID456", privKey)
	if err != nil {
		t.Fatalf("GenerateAppleClientSecret: %v", err)
	}

	// Parse and verify the JWT
	tok, err := jwt.ParseSigned(secret, []jose.SignatureAlgorithm{jose.ES256})
	if err != nil {
		t.Fatalf("parse JWT: %v", err)
	}

	var claims jwt.Claims
	if err := tok.Claims(&privKey.PublicKey, &claims); err != nil {
		t.Fatalf("verify claims: %v", err)
	}

	if claims.Issuer != "TEAMID123" {
		t.Errorf("issuer = %q, want TEAMID123", claims.Issuer)
	}
	if claims.Subject != "com.example.app" {
		t.Errorf("subject = %q, want com.example.app", claims.Subject)
	}
	if len(claims.Audience) != 1 || claims.Audience[0] != "https://appleid.apple.com" {
		t.Errorf("audience = %v, want [https://appleid.apple.com]", claims.Audience)
	}
	if claims.Expiry == nil {
		t.Fatal("expiry is nil")
	}
	expiry := claims.Expiry.Time()
	if expiry.Before(time.Now().Add(179 * 24 * time.Hour)) {
		t.Errorf("expiry %v is too soon", expiry)
	}

	// Verify header has kid
	rawHeader, _ := base64.RawURLEncoding.DecodeString(strings.SplitN(secret, ".", 3)[0])
	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	json.Unmarshal(rawHeader, &header)
	if header.Kid != "KEYID456" {
		t.Errorf("kid = %q, want KEYID456", header.Kid)
	}
	if header.Alg != "ES256" {
		t.Errorf("alg = %q, want ES256", header.Alg)
	}
}

func TestParseP8PrivateKey(t *testing.T) {
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	der, err := x509.MarshalPKCS8PrivateKey(privKey)
	if err != nil {
		t.Fatal(err)
	}

	pemData := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	parsed, err := ParseP8PrivateKey(pemData)
	if err != nil {
		t.Fatalf("ParseP8PrivateKey: %v", err)
	}

	if !parsed.Equal(privKey) {
		t.Error("parsed key does not match original")
	}
}
