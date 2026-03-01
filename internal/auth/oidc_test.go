package auth

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newMockOIDCServer creates a test HTTP server that mimics an OIDC discovery endpoint.
// The issuer in the discovery document is set to the server's own URL so that
// go-oidc's issuer validation passes.
func newMockOIDCServer(t *testing.T) *httptest.Server {
	t.Helper()

	var srv *httptest.Server
	mux := http.NewServeMux()

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		base := srv.URL
		doc := map[string]interface{}{
			"issuer":                                base,
			"authorization_endpoint":                base + "/authorize",
			"token_endpoint":                        base + "/token",
			"jwks_uri":                              base + "/jwks",
			"id_token_signing_alg_values_supported": []string{"ES256"},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(doc)
	})

	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"keys":[]}`))
	})

	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func newTestVerifier() *Verifier {
	return NewVerifier(slog.Default())
}

// TestNewVerifier checks that a freshly created Verifier is non-nil and has no
// providers registered.
func TestNewVerifier(t *testing.T) {
	v := newTestVerifier()
	if v == nil {
		t.Fatal("NewVerifier returned nil")
	}
	// No providers should exist yet.
	_, ok := v.GetProvider("anything")
	if ok {
		t.Error("expected no providers on a new Verifier")
	}
}

// TestAddProvider_Success registers a provider against a real httptest OIDC
// discovery server and expects no error.
func TestAddProvider_Success(t *testing.T) {
	srv := newMockOIDCServer(t)
	v := newTestVerifier()

	cfg := ProviderConfig{
		Name:     "test",
		Issuer:   srv.URL,
		ClientID: "client-abc",
	}

	err := v.AddProvider(context.Background(), cfg)
	if err != nil {
		t.Fatalf("AddProvider returned unexpected error: %v", err)
	}

	_, ok := v.GetProvider("test")
	if !ok {
		t.Error("provider was not registered after successful AddProvider")
	}
}

// TestAddProvider_BadIssuer passes a URL that returns 404 so that OIDC
// discovery fails; the error message must contain "discover OIDC provider".
func TestAddProvider_BadIssuer(t *testing.T) {
	// A server that always 404s for the discovery endpoint.
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()

	v := newTestVerifier()
	cfg := ProviderConfig{
		Name:     "bad",
		Issuer:   srv.URL,
		ClientID: "x",
	}

	err := v.AddProvider(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error for bad issuer, got nil")
	}
	if !strings.Contains(err.Error(), "discover OIDC provider") {
		t.Errorf("error %q does not contain 'discover OIDC provider'", err.Error())
	}
}

// TestGetProvider_Exists verifies that after AddProvider the stored config
// has the same ClientID that was passed in.
func TestGetProvider_Exists(t *testing.T) {
	srv := newMockOIDCServer(t)
	v := newTestVerifier()

	cfg := ProviderConfig{
		Name:     "myprovider",
		Issuer:   srv.URL,
		ClientID: "my-client-id",
	}
	if err := v.AddProvider(context.Background(), cfg); err != nil {
		t.Fatalf("AddProvider: %v", err)
	}

	got, ok := v.GetProvider("myprovider")
	if !ok {
		t.Fatal("GetProvider returned false, expected true")
	}
	if got.ClientID != "my-client-id" {
		t.Errorf("ClientID = %q, want %q", got.ClientID, "my-client-id")
	}
}

// TestGetProvider_NotFound ensures that GetProvider returns false for an
// unregistered provider name.
func TestGetProvider_NotFound(t *testing.T) {
	v := newTestVerifier()
	_, ok := v.GetProvider("nonexistent")
	if ok {
		t.Error("GetProvider should return false for an unknown provider")
	}
}

// TestGetOIDCProvider_Exists checks that the underlying *oidc.Provider is
// non-nil after a successful AddProvider call.
func TestGetOIDCProvider_Exists(t *testing.T) {
	srv := newMockOIDCServer(t)
	v := newTestVerifier()

	cfg := ProviderConfig{
		Name:     "oidcprovider",
		Issuer:   srv.URL,
		ClientID: "cid",
	}
	if err := v.AddProvider(context.Background(), cfg); err != nil {
		t.Fatalf("AddProvider: %v", err)
	}

	p, ok := v.GetOIDCProvider("oidcprovider")
	if !ok {
		t.Fatal("GetOIDCProvider returned false, expected true")
	}
	if p == nil {
		t.Error("GetOIDCProvider returned nil provider")
	}
}

// TestGetOIDCProvider_NotFound ensures GetOIDCProvider returns false for an
// unknown provider name.
func TestGetOIDCProvider_NotFound(t *testing.T) {
	v := newTestVerifier()
	p, ok := v.GetOIDCProvider("ghost")
	if ok {
		t.Error("GetOIDCProvider should return false for an unknown provider")
	}
	if p != nil {
		t.Error("GetOIDCProvider should return nil provider for an unknown name")
	}
}

// TestGetAuthEndpoint verifies that the returned authorization endpoint URL
// contains "/authorize", matching the mock discovery document.
func TestGetAuthEndpoint(t *testing.T) {
	srv := newMockOIDCServer(t)
	v := newTestVerifier()

	cfg := ProviderConfig{
		Name:     "authprovider",
		Issuer:   srv.URL,
		ClientID: "cid",
	}
	if err := v.AddProvider(context.Background(), cfg); err != nil {
		t.Fatalf("AddProvider: %v", err)
	}

	authURL, ok := v.GetAuthEndpoint("authprovider")
	if !ok {
		t.Fatal("GetAuthEndpoint returned false")
	}
	if !strings.Contains(authURL, "/authorize") {
		t.Errorf("auth endpoint %q does not contain '/authorize'", authURL)
	}
}

// TestGetTokenEndpoint verifies that the returned token endpoint URL contains
// "/token", matching the mock discovery document.
func TestGetTokenEndpoint(t *testing.T) {
	srv := newMockOIDCServer(t)
	v := newTestVerifier()

	cfg := ProviderConfig{
		Name:     "tokenprovider",
		Issuer:   srv.URL,
		ClientID: "cid",
	}
	if err := v.AddProvider(context.Background(), cfg); err != nil {
		t.Fatalf("AddProvider: %v", err)
	}

	tokenURL, ok := v.GetTokenEndpoint("tokenprovider")
	if !ok {
		t.Fatal("GetTokenEndpoint returned false")
	}
	if !strings.Contains(tokenURL, "/token") {
		t.Errorf("token endpoint %q does not contain '/token'", tokenURL)
	}
}

// TestVerifyToken_NoProviders confirms that VerifyToken returns an error
// containing "no OIDC providers configured" when called on an empty Verifier.
func TestVerifyToken_NoProviders(t *testing.T) {
	v := newTestVerifier()
	_, err := v.VerifyToken(context.Background(), "sometoken")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "no OIDC providers configured") {
		t.Errorf("error %q does not contain 'no OIDC providers configured'", err.Error())
	}
}

// TestVerifyToken_InvalidToken registers a real (mock) provider and then
// attempts to verify a bogus token, expecting an error containing
// "token verification failed".
func TestVerifyToken_InvalidToken(t *testing.T) {
	srv := newMockOIDCServer(t)
	v := newTestVerifier()

	cfg := ProviderConfig{
		Name:     "verifyprovider",
		Issuer:   srv.URL,
		ClientID: "cid",
	}
	if err := v.AddProvider(context.Background(), cfg); err != nil {
		t.Fatalf("AddProvider: %v", err)
	}

	_, err := v.VerifyToken(context.Background(), "this.is.not.a.valid.token")
	if err == nil {
		t.Fatal("expected verification error for invalid token, got nil")
	}
	if !strings.Contains(err.Error(), "token verification failed") {
		t.Errorf("error %q does not contain 'token verification failed'", err.Error())
	}
}
