package relay

import (
	"testing"
)

func TestGenerateAPIKey(t *testing.T) {
	secret := []byte("test-secret-key-at-least-32-bytes!")

	t.Run("produces non-empty key and keyID", func(t *testing.T) {
		token, keyID, err := GenerateAPIKey(secret, "microsoft", "user@example.com")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if token == "" {
			t.Fatal("expected non-empty token")
		}
		if keyID == "" {
			t.Fatal("expected non-empty keyID")
		}
	})

	t.Run("returns error for empty secret", func(t *testing.T) {
		_, _, err := GenerateAPIKey([]byte{}, "microsoft", "user@example.com")
		if err == nil {
			t.Fatal("expected error for empty secret")
		}
	})

	t.Run("returns error for empty provider", func(t *testing.T) {
		_, _, err := GenerateAPIKey(secret, "", "user@example.com")
		if err == nil {
			t.Fatal("expected error for empty provider")
		}
	})

	t.Run("returns error for empty sub", func(t *testing.T) {
		_, _, err := GenerateAPIKey(secret, "microsoft", "")
		if err == nil {
			t.Fatal("expected error for empty sub")
		}
	})
}

func TestVerifyAPIKey(t *testing.T) {
	secret := []byte("test-secret-key-at-least-32-bytes!")

	t.Run("succeeds with correct secret", func(t *testing.T) {
		token, keyID, err := GenerateAPIKey(secret, "microsoft", "user@example.com")
		if err != nil {
			t.Fatalf("unexpected error generating key: %v", err)
		}

		claims, err := VerifyAPIKey(secret, token)
		if err != nil {
			t.Fatalf("unexpected error verifying key: %v", err)
		}

		if claims.Subject != "microsoft:user@example.com" {
			t.Errorf("expected subject %q, got %q", "microsoft:user@example.com", claims.Subject)
		}
		if claims.KeyID != keyID {
			t.Errorf("expected keyID %q, got %q", keyID, claims.KeyID)
		}
		if claims.Issuer != "phosphor" {
			t.Errorf("expected issuer %q, got %q", "phosphor", claims.Issuer)
		}
	})

	t.Run("fails with wrong secret", func(t *testing.T) {
		token, _, err := GenerateAPIKey(secret, "microsoft", "user@example.com")
		if err != nil {
			t.Fatalf("unexpected error generating key: %v", err)
		}

		wrongSecret := []byte("wrong-secret-key-at-least-32-bytes!")
		_, err = VerifyAPIKey(wrongSecret, token)
		if err == nil {
			t.Fatal("expected error with wrong secret")
		}
	})

	t.Run("fails with malformed token", func(t *testing.T) {
		_, err := VerifyAPIKey(secret, "not-a-valid-jwt")
		if err == nil {
			t.Fatal("expected error with malformed token")
		}
	})

	t.Run("fails with empty token", func(t *testing.T) {
		_, err := VerifyAPIKey(secret, "")
		if err == nil {
			t.Fatal("expected error with empty token")
		}
	})
}

func TestParseAPIKeySubject(t *testing.T) {
	t.Run("splits correctly", func(t *testing.T) {
		provider, sub, err := ParseAPIKeySubject("microsoft:user@example.com")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if provider != "microsoft" {
			t.Errorf("expected provider %q, got %q", "microsoft", provider)
		}
		if sub != "user@example.com" {
			t.Errorf("expected sub %q, got %q", "user@example.com", sub)
		}
	})

	t.Run("handles subject with colons in sub", func(t *testing.T) {
		provider, sub, err := ParseAPIKeySubject("google:urn:some:id")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if provider != "google" {
			t.Errorf("expected provider %q, got %q", "google", provider)
		}
		if sub != "urn:some:id" {
			t.Errorf("expected sub %q, got %q", "urn:some:id", sub)
		}
	})

	t.Run("errors on missing colon", func(t *testing.T) {
		_, _, err := ParseAPIKeySubject("nocolon")
		if err == nil {
			t.Fatal("expected error for missing colon")
		}
	})

	t.Run("errors on empty provider", func(t *testing.T) {
		_, _, err := ParseAPIKeySubject(":sub")
		if err == nil {
			t.Fatal("expected error for empty provider")
		}
	})

	t.Run("errors on empty sub", func(t *testing.T) {
		_, _, err := ParseAPIKeySubject("provider:")
		if err == nil {
			t.Fatal("expected error for empty sub")
		}
	})

	t.Run("errors on empty string", func(t *testing.T) {
		_, _, err := ParseAPIKeySubject("")
		if err == nil {
			t.Fatal("expected error for empty string")
		}
	})
}
