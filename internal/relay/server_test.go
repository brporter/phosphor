package relay

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/brporter/phosphor/internal/auth"
)

func TestNewServer(t *testing.T) {
	store := NewMemorySessionStore()
	hub := NewHub(store, nil, "test", slog.Default())
	store.SetExpiryCallback(func(ctx context.Context, id string) {
		hub.Unregister(ctx, id)
	})
	logger := slog.Default()
	baseURL := "http://test"
	verifier := auth.NewVerifier(slog.Default())
	devMode := true
	authSessions := NewMemoryAuthSessionStore(5 * time.Minute)
	t.Cleanup(authSessions.Stop)

	srv := NewServer(hub, logger, baseURL, verifier, devMode, authSessions)

	if srv == nil {
		t.Fatal("expected non-nil server")
	}
	if srv.hub != hub {
		t.Error("hub not set correctly")
	}
	if srv.logger != logger {
		t.Error("logger not set correctly")
	}
	if srv.baseURL != baseURL {
		t.Errorf("baseURL: got %q, want %q", srv.baseURL, baseURL)
	}
	if srv.devMode != devMode {
		t.Errorf("devMode: got %v, want %v", srv.devMode, devMode)
	}
}

func TestHandler_HealthEndpoint(t *testing.T) {
	store := NewMemorySessionStore()
	hub := NewHub(store, nil, "test", slog.Default())
	authSessions := NewMemoryAuthSessionStore(5 * time.Minute)
	t.Cleanup(authSessions.Stop)

	srv := NewServer(hub, slog.Default(), "http://test", auth.NewVerifier(slog.Default()), true, authSessions)

	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
	if body := rec.Body.String(); body != "ok" {
		t.Errorf("expected body %q, got %q", "ok", body)
	}
}

func TestHandler_RoutesExist(t *testing.T) {
	store := NewMemorySessionStore()
	hub := NewHub(store, nil, "test", slog.Default())
	authSessions := NewMemoryAuthSessionStore(5 * time.Minute)
	t.Cleanup(authSessions.Stop)

	srv := NewServer(hub, slog.Default(), "http://test", auth.NewVerifier(slog.Default()), true, authSessions)

	handler := srv.Handler()

	tests := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/auth/login"},
		{http.MethodGet, "/api/sessions"},
		{http.MethodGet, "/api/auth/poll"},
		{http.MethodGet, "/health"},
	}

	for _, tc := range tests {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code == http.StatusNotFound {
				t.Errorf("route %s %s returned 404, expected non-404", tc.method, tc.path)
			}
		})
	}
}
