package relay

import (
	"errors"
	"log/slog"
	"net/http"
	"testing"

	"github.com/brporter/phosphor/internal/auth"
	"github.com/brporter/phosphor/internal/store"
)

func TestVerifyToken_DevMode_EmptyToken(t *testing.T) {
	s := &Server{devMode: true, logger: slog.Default(), db: store.NewFake()}

	provider, sub, _, err := s.verifyToken(t.Context(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider != "dev" {
		t.Errorf("provider = %q, want %q", provider, "dev")
	}
	if sub != "anonymous" {
		t.Errorf("sub = %q, want %q", sub, "anonymous")
	}
}

func TestVerifyToken_DevMode_ColonFormat(t *testing.T) {
	s := &Server{devMode: true, logger: slog.Default(), db: store.NewFake()}

	provider, sub, _, err := s.verifyToken(t.Context(), "prov:sub")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider != "prov" {
		t.Errorf("provider = %q, want %q", provider, "prov")
	}
	if sub != "sub" {
		t.Errorf("sub = %q, want %q", sub, "sub")
	}
}

func TestVerifyToken_DevMode_SinglePart(t *testing.T) {
	// "noseparator" has no colon, so the colon-format branch is skipped.
	// verifier is nil, so the OIDC branch is skipped.
	// devMode is true, so the final devMode fallback fires.
	s := &Server{devMode: true, logger: slog.Default(), db: store.NewFake()}

	provider, sub, _, err := s.verifyToken(t.Context(), "noseparator")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider != "dev" {
		t.Errorf("provider = %q, want %q", provider, "dev")
	}
	if sub != "anonymous" {
		t.Errorf("sub = %q, want %q", sub, "anonymous")
	}
}

func TestVerifyToken_NonDev_EmptyToken(t *testing.T) {
	s := &Server{devMode: false, logger: slog.Default(), db: store.NewFake()}

	_, _, _, err := s.verifyToken(t.Context(), "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, auth.ErrNoToken) {
		t.Errorf("err = %v, want auth.ErrNoToken", err)
	}
}

func TestExtractIdentity_BearerToken(t *testing.T) {
	s := &Server{
		devMode:  true,
		logger:   slog.Default(),
		verifier: auth.NewVerifier(slog.Default()),
		db:       store.NewFake(),
	}

	r, err := http.NewRequest(http.MethodGet, "/api/sessions", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	r.Header.Set("Authorization", "Bearer test:user")

	provider, sub, _, err := s.extractIdentity(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider != "test" {
		t.Errorf("provider = %q, want %q", provider, "test")
	}
	if sub != "user" {
		t.Errorf("sub = %q, want %q", sub, "user")
	}
}

func TestVerifyToken_APIKey_Valid(t *testing.T) {
	secret := []byte("test-secret-32-bytes-long-enough")
	rawJWT, keyID, err := GenerateAPIKey(secret, "microsoft", "user123")
	if err != nil {
		t.Fatalf("GenerateAPIKey: %v", err)
	}

	db := store.NewFake()
	user, err := db.GetOrCreateUser(t.Context(), "microsoft", "user123", "")
	if err != nil {
		t.Fatalf("GetOrCreateUser: %v", err)
	}
	if err := db.RecordAPIKey(t.Context(), keyID, user.ID); err != nil {
		t.Fatalf("RecordAPIKey: %v", err)
	}

	s := &Server{
		devMode:      false,
		logger:       slog.Default(),
		apiKeySecret: secret,
		db:           db,
	}

	provider, sub, _, err := s.verifyToken(t.Context(), "phk:"+rawJWT)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider != "microsoft" {
		t.Errorf("provider = %q, want %q", provider, "microsoft")
	}
	if sub != "user123" {
		t.Errorf("sub = %q, want %q", sub, "user123")
	}
}

func TestVerifyToken_APIKey_Revoked(t *testing.T) {
	secret := []byte("test-secret-32-bytes-long-enough")
	rawJWT, keyID, err := GenerateAPIKey(secret, "microsoft", "user123")
	if err != nil {
		t.Fatalf("GenerateAPIKey: %v", err)
	}

	db := store.NewFake()
	user, err := db.GetOrCreateUser(t.Context(), "microsoft", "user123", "")
	if err != nil {
		t.Fatalf("GetOrCreateUser: %v", err)
	}
	if err := db.RecordAPIKey(t.Context(), keyID, user.ID); err != nil {
		t.Fatalf("RecordAPIKey: %v", err)
	}
	if err := db.RevokeAPIKey(t.Context(), keyID, user.ID); err != nil {
		t.Fatalf("RevokeAPIKey: %v", err)
	}

	s := &Server{
		devMode:      false,
		logger:       slog.Default(),
		apiKeySecret: secret,
		db:           db,
	}

	_, _, _, err = s.verifyToken(t.Context(), "phk:"+rawJWT)
	if err == nil {
		t.Fatal("expected error for revoked key, got nil")
	}
	if err.Error() != "api key revoked" {
		t.Errorf("error = %q, want %q", err.Error(), "api key revoked")
	}
}

func TestVerifyToken_APIKey_Unknown(t *testing.T) {
	// Keys minted before the database existed (or never recorded) are
	// treated as revoked.
	secret := []byte("test-secret-32-bytes-long-enough")
	rawJWT, _, err := GenerateAPIKey(secret, "microsoft", "user123")
	if err != nil {
		t.Fatalf("GenerateAPIKey: %v", err)
	}

	s := &Server{
		devMode:      false,
		logger:       slog.Default(),
		apiKeySecret: secret,
		db:           store.NewFake(),
	}

	_, _, _, err = s.verifyToken(t.Context(), "phk:"+rawJWT)
	if err == nil {
		t.Fatal("expected error for unknown key, got nil")
	}
}

func TestVerifyToken_APIKey_DevMode(t *testing.T) {
	s := &Server{
		devMode: true,
		logger:  slog.Default(),
		db:      store.NewFake(),
	}

	provider, sub, _, err := s.verifyToken(t.Context(), "phk:anything")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider != "dev" {
		t.Errorf("provider = %q, want %q", provider, "dev")
	}
	if sub != "anonymous" {
		t.Errorf("sub = %q, want %q", sub, "anonymous")
	}
}

func TestVerifyToken_APIKey_WrongSecret(t *testing.T) {
	secret1 := []byte("secret-one-32-bytes-long-enough!")
	secret2 := []byte("secret-two-32-bytes-long-enough!")

	rawJWT, _, err := GenerateAPIKey(secret1, "microsoft", "user123")
	if err != nil {
		t.Fatalf("GenerateAPIKey: %v", err)
	}

	s := &Server{
		devMode:      false,
		logger:       slog.Default(),
		apiKeySecret: secret2,
		db:           store.NewFake(),
	}

	_, _, _, err = s.verifyToken(t.Context(), "phk:"+rawJWT)
	if err == nil {
		t.Fatal("expected error for wrong secret, got nil")
	}
}
