package auth

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// handlerCapture is a simple handler that records the identity injected into
// the request context and writes a 200 OK response.
type handlerCapture struct {
	identity *Identity
	called   bool
}

func (h *handlerCapture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.called = true
	h.identity = IdentityFromContext(r.Context())
	w.WriteHeader(http.StatusOK)
}

func newRequest(authHeader string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if authHeader != "" {
		r.Header.Set("Authorization", authHeader)
	}
	return r
}

// TestMiddleware_DevMode_NoToken verifies that in dev mode a request with no
// Authorization header is allowed through and receives Identity{Provider:"dev",
// Sub:"anonymous"}.
func TestMiddleware_DevMode_NoToken(t *testing.T) {
	v := NewVerifier(slog.Default())
	capture := &handlerCapture{}
	handler := Middleware(v, true)(capture)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, newRequest(""))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if !capture.called {
		t.Fatal("next handler was not called")
	}
	if capture.identity == nil {
		t.Fatal("identity is nil, want non-nil")
	}
	if capture.identity.Provider != "dev" {
		t.Errorf("Provider = %q, want \"dev\"", capture.identity.Provider)
	}
	if capture.identity.Sub != "anonymous" {
		t.Errorf("Sub = %q, want \"anonymous\"", capture.identity.Sub)
	}
}

// TestMiddleware_DevMode_ColonToken verifies that in dev mode a Bearer token of
// the form "prov:sub" (which fails real OIDC verification because no providers
// are registered) is parsed into Identity{Provider:"prov", Sub:"sub"}.
func TestMiddleware_DevMode_ColonToken(t *testing.T) {
	v := NewVerifier(slog.Default())
	capture := &handlerCapture{}
	handler := Middleware(v, true)(capture)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, newRequest("Bearer prov:sub"))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if !capture.called {
		t.Fatal("next handler was not called")
	}
	if capture.identity == nil {
		t.Fatal("identity is nil, want non-nil")
	}
	if capture.identity.Provider != "prov" {
		t.Errorf("Provider = %q, want \"prov\"", capture.identity.Provider)
	}
	if capture.identity.Sub != "sub" {
		t.Errorf("Sub = %q, want \"sub\"", capture.identity.Sub)
	}
}

// TestMiddleware_DevMode_InvalidColonToken verifies that in dev mode a Bearer
// token without a colon (and no real providers registered) results in a 401.
func TestMiddleware_DevMode_InvalidColonToken(t *testing.T) {
	v := NewVerifier(slog.Default())
	capture := &handlerCapture{}
	handler := Middleware(v, true)(capture)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, newRequest("Bearer nocolon"))

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if capture.called {
		t.Fatal("next handler should not have been called")
	}
	body := strings.TrimSpace(w.Body.String())
	if !strings.Contains(body, "invalid token") {
		t.Errorf("body = %q, want to contain \"invalid token\"", body)
	}
}

// TestMiddleware_NonDev_NoToken verifies that outside dev mode a request with
// no Authorization header gets a 401 with "missing authorization".
func TestMiddleware_NonDev_NoToken(t *testing.T) {
	v := NewVerifier(slog.Default())
	capture := &handlerCapture{}
	handler := Middleware(v, false)(capture)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, newRequest(""))

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if capture.called {
		t.Fatal("next handler should not have been called")
	}
	body := strings.TrimSpace(w.Body.String())
	if !strings.Contains(body, "missing authorization") {
		t.Errorf("body = %q, want to contain \"missing authorization\"", body)
	}
}

// TestMiddleware_NonDev_InvalidToken verifies that outside dev mode a Bearer
// token that fails OIDC verification results in a 401 with "invalid token".
func TestMiddleware_NonDev_InvalidToken(t *testing.T) {
	v := NewVerifier(slog.Default())
	capture := &handlerCapture{}
	handler := Middleware(v, false)(capture)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, newRequest("Bearer garbage"))

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if capture.called {
		t.Fatal("next handler should not have been called")
	}
	body := strings.TrimSpace(w.Body.String())
	if !strings.Contains(body, "invalid token") {
		t.Errorf("body = %q, want to contain \"invalid token\"", body)
	}
}

// TestIdentityFromContext_Nil verifies that IdentityFromContext returns nil
// when no identity has been stored in the context.
func TestIdentityFromContext_Nil(t *testing.T) {
	id := IdentityFromContext(context.Background())
	if id != nil {
		t.Fatalf("expected nil, got %+v", id)
	}
}

// TestIdentityFromContext_RoundTrip verifies the full round-trip: the
// middleware stores the identity in the context and IdentityFromContext
// retrieves the same value inside the downstream handler.
func TestIdentityFromContext_RoundTrip(t *testing.T) {
	v := NewVerifier(slog.Default())

	var retrieved *Identity
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		retrieved = IdentityFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	handler := Middleware(v, true)(next)

	w := httptest.NewRecorder()
	// No Authorization header — dev mode yields Identity{Provider:"dev", Sub:"anonymous"}.
	handler.ServeHTTP(w, newRequest(""))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if retrieved == nil {
		t.Fatal("IdentityFromContext returned nil inside handler")
	}
	if retrieved.Provider != "dev" || retrieved.Sub != "anonymous" {
		t.Errorf("identity = %+v, want {Provider:\"dev\", Sub:\"anonymous\"}", retrieved)
	}
}
