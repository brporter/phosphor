package relay

import (
	"context"
	"time"
)

// AuthSessionData holds a pending browser-based OIDC auth flow.
type AuthSessionData struct {
	ID           string
	Provider     string
	CodeVerifier string
	Source       string // "web" or "cli"
	IDToken      string
	CreatedAt    time.Time
}

// AuthSessionStoreI manages pending auth sessions. Implementation: MemoryAuthSessionStore.
type AuthSessionStoreI interface {
	Create(ctx context.Context, provider, codeVerifier, source string) (AuthSessionData, error)
	Get(ctx context.Context, id string) (AuthSessionData, bool, error)
	SetProvider(ctx context.Context, id, provider, codeVerifier string) error
	Complete(ctx context.Context, id, idToken string) error
	Consume(ctx context.Context, id string) (string, bool, error)
	Stop()
}
