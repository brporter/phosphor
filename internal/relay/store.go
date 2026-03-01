package relay

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"time"
)

// SessionInfo holds serializable metadata for a terminal sharing session.
// Stored in SessionStore (Redis hash or in-memory map).
type SessionInfo struct {
	ID             string
	OwnerProvider  string
	OwnerSub       string
	Mode           string // "pty" or "pipe"
	Command        string
	ReconnectToken string
	RelayID        string
	Cols           int
	Rows           int
	Disconnected   bool
	DisconnectedAt time.Time
	ProcessExited  bool
}

// SessionStore persists session metadata. Implementations: MemorySessionStore, RedisSessionStore.
type SessionStore interface {
	Register(ctx context.Context, info SessionInfo) error
	Unregister(ctx context.Context, sessionID string) error
	Get(ctx context.Context, sessionID string) (SessionInfo, bool, error)
	ListForOwner(ctx context.Context, provider, sub string) ([]SessionInfo, error)
	SetDisconnected(ctx context.Context, sessionID string) error
	SetReconnected(ctx context.Context, sessionID, newToken, relayID string) error
	UpdateDimensions(ctx context.Context, sessionID string, cols, rows int) error
	ScheduleExpiry(ctx context.Context, sessionID string, grace time.Duration) error
	CancelExpiry(ctx context.Context, sessionID string) error
	SetProcessExited(ctx context.Context, sessionID string, exited bool) error
}

// MessageBus provides cross-relay pub/sub messaging. Implementations: MemoryMessageBus, RedisMessageBus.
type MessageBus interface {
	Publish(ctx context.Context, channel string, msg []byte) error
	Subscribe(ctx context.Context, channel string) (<-chan []byte, func(), error)
}

// OutputChannel returns the pub/sub channel name for CLI stdout.
func OutputChannel(sessionID string) string { return "stream:" + sessionID + ":output" }

// InputChannel returns the pub/sub channel name for viewer stdin/resize.
func InputChannel(sessionID string) string { return "stream:" + sessionID + ":input" }

// AuthSessionData holds a pending browser-based auth flow.
type AuthSessionData struct {
	ID           string
	Provider     string
	CodeVerifier string
	Source       string // "web" or "cli"
	IDToken      string
	CreatedAt    time.Time
}

// AuthSessionStoreI manages pending auth sessions. Implementations: MemoryAuthSessionStore, RedisAuthSessionStore.
type AuthSessionStoreI interface {
	Create(ctx context.Context, provider, codeVerifier, source string) (AuthSessionData, error)
	Get(ctx context.Context, id string) (AuthSessionData, bool, error)
	SetProvider(ctx context.Context, id, provider, codeVerifier string) error
	Complete(ctx context.Context, id, idToken string) error
	Consume(ctx context.Context, id string) (string, bool, error)
	Stop()
}

// generateToken produces a cryptographically random URL-safe token.
func generateToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
