package auth

import (
	"context"
	"net/http"
	"strings"
)

type contextKey string

const identityKey contextKey = "identity"

// Middleware validates the Authorization header and injects Identity into context.
func Middleware(v *Verifier, devMode bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Get("Authorization")
			token := strings.TrimPrefix(auth, "Bearer ")

			if token == "" || token == auth {
				if devMode {
					// Dev mode: allow anonymous
					id := &Identity{Provider: "dev", Sub: "anonymous"}
					ctx := context.WithValue(r.Context(), identityKey, id)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
				http.Error(w, `{"error":"missing authorization"}`, http.StatusUnauthorized)
				return
			}

			id, err := v.VerifyToken(r.Context(), token)
			if err != nil {
				if devMode {
					// Dev mode fallback: parse as provider:sub
					parts := strings.SplitN(token, ":", 2)
					if len(parts) == 2 {
						id = &Identity{Provider: parts[0], Sub: parts[1]}
						ctx := context.WithValue(r.Context(), identityKey, id)
						next.ServeHTTP(w, r.WithContext(ctx))
						return
					}
				}
				http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), identityKey, id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// IdentityFromContext extracts the Identity from request context.
func IdentityFromContext(ctx context.Context) *Identity {
	id, _ := ctx.Value(identityKey).(*Identity)
	return id
}
