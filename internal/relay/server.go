package relay

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/brporter/phosphor/internal/auth"
	"github.com/brporter/phosphor/internal/store"
)

// DataStore is the durable-state surface the relay needs. It is implemented
// by *store.Store (Postgres) and *store.Fake (tests).
type DataStore interface {
	GetOrCreateUser(ctx context.Context, provider, subject, email string) (*store.User, error)
	GetUserByID(ctx context.Context, id uuid.UUID) (*store.User, error)
	CreateMachine(ctx context.Context, tenantID uuid.UUID, name, hostname, fingerprint string) (*store.Machine, error)
	GetMachine(ctx context.Context, id uuid.UUID) (*store.Machine, error)
	GetMachineByFingerprint(ctx context.Context, fingerprint string) (*store.Machine, error)
	ListMachines(ctx context.Context, tenantID uuid.UUID) ([]*store.Machine, error)
	TouchMachine(ctx context.Context, id uuid.UUID) error
	RenameMachine(ctx context.Context, id uuid.UUID, name string) error
	DeleteMachine(ctx context.Context, id uuid.UUID) error
	RecordAPIKey(ctx context.Context, keyID string, userID uuid.UUID) error
	IsAPIKeyRevoked(ctx context.Context, keyID string) (bool, error)
	RevokeAPIKey(ctx context.Context, keyID string, userID uuid.UUID) error
}

// Server is the relay HTTP server.
type Server struct {
	hub          *Hub
	logger       *slog.Logger
	baseURL      string
	verifier     *auth.Verifier
	devMode      bool
	authSessions AuthSessionStoreI
	apiKeySecret []byte
	db           DataStore
	gracePeriod  time.Duration
}

// NewServer creates a new relay server.
func NewServer(hub *Hub, logger *slog.Logger, baseURL string, verifier *auth.Verifier, devMode bool, authSessions AuthSessionStoreI, apiKeySecret []byte, db DataStore, gracePeriod time.Duration) *Server {
	return &Server{hub: hub, logger: logger, baseURL: baseURL, verifier: verifier, devMode: devMode, authSessions: authSessions, apiKeySecret: apiKeySecret, db: db, gracePeriod: gracePeriod}
}

// Handler returns the HTTP handler with all routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// WebSocket endpoints (auth handled in-protocol via Hello/Join messages)
	mux.HandleFunc("GET /ws/cli", s.HandleCLIWebSocket)
	mux.HandleFunc("GET /ws/view/{id}", s.HandleViewerWebSocket)

	// REST API (auth via middleware)
	mux.HandleFunc("GET /api/sessions", s.HandleListSessions)
	mux.HandleFunc("DELETE /api/sessions/{id}", s.HandleDestroySession)

	// Auth flow endpoints
	mux.HandleFunc("GET /api/auth/config", s.HandleAuthConfig)
	mux.HandleFunc("POST /api/auth/login", s.HandleAuthLogin)
	mux.HandleFunc("GET /api/auth/authorize", s.HandleAuthAuthorize)
	mux.HandleFunc("GET /api/auth/callback", s.HandleAuthCallback)
	mux.HandleFunc("POST /api/auth/callback", s.HandleAuthCallback)
	mux.HandleFunc("GET /api/auth/poll", s.HandleAuthPoll)
	mux.HandleFunc("POST /api/auth/api-key", s.HandleGenerateAPIKey)

	// CLI provider-picker auth flow
	mux.HandleFunc("POST /api/auth/cli-start", s.HandleCLIStart)
	mux.HandleFunc("GET /api/auth/cli-login", s.HandleCLILogin)
	mux.HandleFunc("POST /api/auth/cli-choose", s.HandleCLIChoose)

	// Health check
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	// Static files (SPA) — served last as catch-all
	mux.Handle("/", s.StaticHandler())

	return mux
}
