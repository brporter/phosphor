package auth

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/coreos/go-oidc/v3/oidc"
)

// ProviderConfig holds OIDC configuration for a single identity provider.
type ProviderConfig struct {
	Name          string // "microsoft", "google", "apple"
	Issuer        string
	ClientID      string
	ClientSecret  string // empty for public clients (CLI)
	DeviceAuthURL string // for device code flow
	// Apple-specific fields
	TeamID     string            // Apple Developer Team ID
	KeyID      string            // Apple key ID
	PrivateKey *ecdsa.PrivateKey // Apple P8 signing key
}

// ErrNoToken is returned when no auth token is provided.
var ErrNoToken = errors.New("no authentication token provided")

// Identity represents a verified user.
type Identity struct {
	Provider string // provider name
	Sub      string // OIDC subject claim
	Email    string // optional
}

// Verifier validates tokens from multiple OIDC providers.
type Verifier struct {
	mu        sync.RWMutex
	providers map[string]*providerEntry
	logger    *slog.Logger
}

type providerEntry struct {
	config   ProviderConfig
	provider *oidc.Provider
	verifier *oidc.IDTokenVerifier
}

// NewVerifier creates a multi-provider token verifier.
func NewVerifier(logger *slog.Logger) *Verifier {
	return &Verifier{
		providers: make(map[string]*providerEntry),
		logger:    logger,
	}
}

// AddProvider registers an OIDC provider. Call during startup.
func (v *Verifier) AddProvider(ctx context.Context, cfg ProviderConfig) error {
	provider, err := oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return fmt.Errorf("discover OIDC provider %s: %w", cfg.Name, err)
	}

	verifier := provider.Verifier(&oidc.Config{
		ClientID: cfg.ClientID,
	})

	v.mu.Lock()
	defer v.mu.Unlock()
	v.providers[cfg.Name] = &providerEntry{
		config:   cfg,
		provider: provider,
		verifier: verifier,
	}

	v.logger.Info("OIDC provider registered", "name", cfg.Name, "issuer", cfg.Issuer)
	return nil
}

// VerifyToken verifies an ID token and returns the identity.
// It tries all registered providers until one succeeds.
func (v *Verifier) VerifyToken(ctx context.Context, rawToken string) (*Identity, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()

	var lastErr error
	for name, entry := range v.providers {
		idToken, err := entry.verifier.Verify(ctx, rawToken)
		if err != nil {
			lastErr = err
			continue
		}

		var claims struct {
			Email string `json:"email"`
		}
		idToken.Claims(&claims)

		return &Identity{
			Provider: name,
			Sub:      idToken.Subject,
			Email:    claims.Email,
		}, nil
	}

	if lastErr != nil {
		return nil, fmt.Errorf("token verification failed: %w", lastErr)
	}
	return nil, errors.New("no OIDC providers configured")
}

// GetProvider returns the config for a named provider.
func (v *Verifier) GetProvider(name string) (ProviderConfig, bool) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	entry, ok := v.providers[name]
	if !ok {
		return ProviderConfig{}, false
	}
	return entry.config, true
}

// GetOIDCProvider returns the go-oidc provider for device code flow usage.
func (v *Verifier) GetOIDCProvider(name string) (*oidc.Provider, bool) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	entry, ok := v.providers[name]
	if !ok {
		return nil, false
	}
	return entry.provider, true
}

// GetAuthEndpoint returns the authorization endpoint for a named provider.
func (v *Verifier) GetAuthEndpoint(name string) (string, bool) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	entry, ok := v.providers[name]
	if !ok {
		return "", false
	}
	var claims struct {
		AuthURL string `json:"authorization_endpoint"`
	}
	if err := entry.provider.Claims(&claims); err != nil {
		return "", false
	}
	return claims.AuthURL, true
}

// GetTokenEndpoint returns the token endpoint for a named provider.
func (v *Verifier) GetTokenEndpoint(name string) (string, bool) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	entry, ok := v.providers[name]
	if !ok {
		return "", false
	}
	var claims struct {
		TokenURL string `json:"token_endpoint"`
	}
	if err := entry.provider.Claims(&claims); err != nil {
		return "", false
	}
	return claims.TokenURL, true
}
