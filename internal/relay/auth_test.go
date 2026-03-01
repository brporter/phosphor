package relay

import (
	"errors"
	"log/slog"
	"net/http"
	"testing"

	"github.com/brporter/phosphor/internal/auth"
)

func TestVerifyToken_DevMode_EmptyToken(t *testing.T) {
	s := &Server{devMode: true, logger: slog.Default()}

	provider, sub, err := s.verifyToken(t.Context(), "")
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
	s := &Server{devMode: true, logger: slog.Default()}

	provider, sub, err := s.verifyToken(t.Context(), "prov:sub")
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
	s := &Server{devMode: true, logger: slog.Default()}

	provider, sub, err := s.verifyToken(t.Context(), "noseparator")
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
	s := &Server{devMode: false, logger: slog.Default()}

	_, _, err := s.verifyToken(t.Context(), "")
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
	}

	r, err := http.NewRequest(http.MethodGet, "/api/sessions", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	r.Header.Set("Authorization", "Bearer test:user")

	provider, sub, err := s.extractIdentity(r)
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
