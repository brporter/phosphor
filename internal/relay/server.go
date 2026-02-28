package relay

import (
	"log/slog"
	"net/http"

	"github.com/brporter/phosphor/internal/auth"
)

// Server is the relay HTTP server.
type Server struct {
	hub      *Hub
	logger   *slog.Logger
	baseURL  string
	verifier *auth.Verifier
	devMode  bool
}

// NewServer creates a new relay server.
func NewServer(hub *Hub, logger *slog.Logger, baseURL string, verifier *auth.Verifier, devMode bool) *Server {
	return &Server{hub: hub, logger: logger, baseURL: baseURL, verifier: verifier, devMode: devMode}
}

// Handler returns the HTTP handler with all routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// WebSocket endpoints (auth handled in-protocol via Hello/Join messages)
	mux.HandleFunc("GET /ws/cli", s.HandleCLIWebSocket)
	mux.HandleFunc("GET /ws/view/{id}", s.HandleViewerWebSocket)

	// REST API (auth via middleware)
	mux.HandleFunc("GET /api/sessions", s.HandleListSessions)

	// Health check
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	// Static files (SPA) — served last as catch-all
	mux.Handle("/", s.StaticHandler())

	return mux
}
