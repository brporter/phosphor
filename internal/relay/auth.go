package relay

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/brporter/phosphor/internal/auth"
)

// verifyToken validates an auth token and returns (provider, sub, email, err).
func (s *Server) verifyToken(ctx context.Context, token string) (string, string, string, error) {
	if token == "" && s.devMode {
		return "dev", "anonymous", "", nil
	}

	// API key authentication (tokens prefixed with "phk:")
	if strings.HasPrefix(token, "phk:") {
		if s.devMode {
			return "dev", "anonymous", "", nil
		}
		raw := strings.TrimPrefix(token, "phk:")
		claims, err := VerifyAPIKey(s.apiKeySecret, raw)
		if err != nil {
			return "", "", "", fmt.Errorf("invalid api key: %w", err)
		}
		if s.blocklist != nil && s.blocklist.IsRevoked(claims.KeyID) {
			return "", "", "", fmt.Errorf("api key revoked")
		}
		provider, sub, err := ParseAPIKeySubject(claims.Subject)
		if err != nil {
			return "", "", "", err
		}
		return provider, sub, "", nil
	}

	// Dev-mode fallback: parse token as "provider:sub"
	if s.devMode {
		parts := strings.SplitN(token, ":", 2)
		if len(parts) == 2 {
			return parts[0], parts[1], "", nil
		}
	}

	// Real OIDC verification
	if s.verifier != nil {
		id, err := s.verifier.VerifyToken(ctx, token)
		if err == nil {
			return id.Provider, id.Sub, id.Email, nil
		}
		// In dev mode, fall through to accept any token
		if !s.devMode {
			return "", "", "", err
		}
	}

	if s.devMode {
		return "dev", "anonymous", "", nil
	}

	return "", "", "", auth.ErrNoToken
}

// extractIdentity extracts the user identity from the request.
func (s *Server) extractIdentity(r *http.Request) (string, string, string, error) {
	hdr := r.Header.Get("Authorization")
	token := strings.TrimPrefix(hdr, "Bearer ")
	if token == hdr {
		token = "" // no "Bearer " prefix found
	}
	return s.verifyToken(r.Context(), token)
}
