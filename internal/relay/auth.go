package relay

import (
	"context"
	"net/http"
	"strings"

	"github.com/brporter/phosphor/internal/auth"
)

// verifyToken validates an auth token and returns (provider, sub).
func (s *Server) verifyToken(ctx context.Context, token string) (string, string, error) {
	if token == "" && s.devMode {
		return "dev", "anonymous", nil
	}

	// Dev-mode fallback: parse token as "provider:sub"
	if s.devMode {
		parts := strings.SplitN(token, ":", 2)
		if len(parts) == 2 {
			return parts[0], parts[1], nil
		}
	}

	// Real OIDC verification
	if s.verifier != nil {
		id, err := s.verifier.VerifyToken(ctx, token)
		if err != nil {
			return "", "", err
		}
		return id.Provider, id.Sub, nil
	}

	if s.devMode {
		return "dev", "anonymous", nil
	}

	return "", "", auth.ErrNoToken
}

// extractIdentity extracts the user identity from the request.
func (s *Server) extractIdentity(r *http.Request) (string, string, error) {
	hdr := r.Header.Get("Authorization")
	token := strings.TrimPrefix(hdr, "Bearer ")
	if token == hdr {
		token = "" // no "Bearer " prefix found
	}
	return s.verifyToken(r.Context(), token)
}
