package relay

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/brporter/phosphor/internal/auth"
)

// newTestAuthServer creates a *Server configured with a mock OIDC provider.
// The mock OIDC httptest.Server serves discovery, JWKS, and token endpoints.
func newTestAuthServer(t *testing.T) *Server {
	t.Helper()

	mux := http.NewServeMux()
	oidcServer := httptest.NewServer(mux)
	t.Cleanup(oidcServer.Close)

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                oidcServer.URL,
			"authorization_endpoint":                oidcServer.URL + "/authorize",
			"token_endpoint":                        oidcServer.URL + "/token",
			"jwks_uri":                              oidcServer.URL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"ES256"},
		})
	})

	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"keys":[]}`))
	})

	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"id_token": "mock-id-token-value"})
	})

	hub := NewHub(slog.Default())
	verifier := auth.NewVerifier(slog.Default())
	err := verifier.AddProvider(context.Background(), auth.ProviderConfig{
		Name:     "test",
		Issuer:   oidcServer.URL,
		ClientID: "test-client-id",
	})
	if err != nil {
		t.Fatal(err)
	}

	return NewServer(hub, slog.Default(), "http://localhost:8080", verifier, true)
}

// --- PKCE helper tests ---

func TestGenerateCodeVerifier(t *testing.T) {
	v := generateCodeVerifier()
	// 32 random bytes base64url-encoded (no padding) = 43 characters.
	if len(v) != 43 {
		t.Errorf("generateCodeVerifier() length = %d, want 43", len(v))
	}

	// Two consecutive calls must produce different values.
	v2 := generateCodeVerifier()
	if v == v2 {
		t.Error("generateCodeVerifier() returned the same value twice; expected randomness")
	}
}

func TestCodeChallenge(t *testing.T) {
	verifier := "test-verifier-value"
	challenge := codeChallenge(verifier)

	// SHA-256 produces 32 bytes; base64url (no padding) encodes them as 43 chars.
	if len(challenge) != 43 {
		t.Errorf("codeChallenge() length = %d, want 43", len(challenge))
	}
}

func TestCodeChallenge_Deterministic(t *testing.T) {
	verifier := "deterministic-input"
	c1 := codeChallenge(verifier)
	c2 := codeChallenge(verifier)

	if c1 != c2 {
		t.Errorf("codeChallenge() not deterministic: got %q then %q", c1, c2)
	}
}

// --- HandleAuthLogin ---

func TestHandleAuthLogin_Success(t *testing.T) {
	s := newTestAuthServer(t)

	body := strings.NewReader(`{"provider":"test"}`)
	r := httptest.NewRequest(http.MethodPost, "/api/auth/login", body)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.HandleAuthLogin(w, r)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var result authLoginResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if result.SessionID == "" {
		t.Error("session_id is empty")
	}
	if result.AuthURL == "" {
		t.Error("auth_url is empty")
	}

	// auth_url must contain the session ID and the expected path.
	if !strings.Contains(result.AuthURL, result.SessionID) {
		t.Errorf("auth_url %q does not contain session_id %q", result.AuthURL, result.SessionID)
	}
	if !strings.Contains(result.AuthURL, "/api/auth/authorize") {
		t.Errorf("auth_url %q does not contain /api/auth/authorize", result.AuthURL)
	}
}

func TestHandleAuthLogin_InvalidBody(t *testing.T) {
	s := newTestAuthServer(t)

	r := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader("not-json"))
	w := httptest.NewRecorder()

	s.HandleAuthLogin(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestHandleAuthLogin_UnknownProvider(t *testing.T) {
	s := newTestAuthServer(t)

	body := strings.NewReader(`{"provider":"nonexistent"}`)
	r := httptest.NewRequest(http.MethodPost, "/api/auth/login", body)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.HandleAuthLogin(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}

	if !strings.Contains(w.Body.String(), "unknown provider") {
		t.Errorf("response body %q does not contain 'unknown provider'", w.Body.String())
	}
}

// --- HandleAuthAuthorize ---

func TestHandleAuthAuthorize_Success(t *testing.T) {
	s := newTestAuthServer(t)

	// Create a real session in the store first.
	sess := s.authSessions.Create("test", "test-code-verifier")

	r := httptest.NewRequest(http.MethodGet, "/api/auth/authorize?session="+sess.ID, nil)
	w := httptest.NewRecorder()

	s.HandleAuthAuthorize(w, r)

	resp := w.Result()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}

	location := resp.Header.Get("Location")
	if location == "" {
		t.Fatal("Location header is empty")
	}

	parsed, err := url.Parse(location)
	if err != nil {
		t.Fatalf("parse Location %q: %v", location, err)
	}

	q := parsed.Query()

	if q.Get("client_id") != "test-client-id" {
		t.Errorf("client_id = %q, want test-client-id", q.Get("client_id"))
	}
	if q.Get("response_type") != "code" {
		t.Errorf("response_type = %q, want code", q.Get("response_type"))
	}
	if q.Get("state") != sess.ID {
		t.Errorf("state = %q, want %q", q.Get("state"), sess.ID)
	}
	if q.Get("code_challenge") == "" {
		t.Error("code_challenge is empty")
	}
	if q.Get("code_challenge_method") != "S256" {
		t.Errorf("code_challenge_method = %q, want S256", q.Get("code_challenge_method"))
	}

	// Verify redirect_uri points to the server's callback.
	if !strings.Contains(q.Get("redirect_uri"), "/api/auth/callback") {
		t.Errorf("redirect_uri %q does not contain /api/auth/callback", q.Get("redirect_uri"))
	}
}

func TestHandleAuthAuthorize_InvalidSession(t *testing.T) {
	s := newTestAuthServer(t)

	r := httptest.NewRequest(http.MethodGet, "/api/auth/authorize?session=bad-session-id", nil)
	w := httptest.NewRecorder()

	s.HandleAuthAuthorize(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// --- HandleAuthCallback ---

func TestHandleAuthCallback_MissingCode(t *testing.T) {
	s := newTestAuthServer(t)

	r := httptest.NewRequest(http.MethodGet, "/api/auth/callback", nil)
	w := httptest.NewRecorder()

	s.HandleAuthCallback(w, r)

	resp := w.Result()
	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	if !strings.Contains(bodyStr, "missing code or state") {
		t.Errorf("body %q does not contain 'missing code or state'", bodyStr)
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html prefix", ct)
	}
}

func TestHandleAuthCallback_InvalidSession(t *testing.T) {
	s := newTestAuthServer(t)

	r := httptest.NewRequest(http.MethodGet, "/api/auth/callback?code=auth-code&state=nonexistent-session", nil)
	w := httptest.NewRecorder()

	s.HandleAuthCallback(w, r)

	body, _ := io.ReadAll(w.Result().Body)
	bodyStr := string(body)

	if !strings.Contains(bodyStr, "session expired") {
		t.Errorf("body %q does not contain 'session expired'", bodyStr)
	}
}

func TestHandleAuthCallback_Success(t *testing.T) {
	s := newTestAuthServer(t)

	// Pre-create an auth session so the handler can look it up.
	sess := s.authSessions.Create("test", "test-code-verifier")

	target := "/api/auth/callback?code=auth-code&state=" + sess.ID
	r := httptest.NewRequest(http.MethodGet, target, nil)
	w := httptest.NewRecorder()

	s.HandleAuthCallback(w, r)

	body, _ := io.ReadAll(w.Result().Body)
	bodyStr := string(body)

	if !strings.Contains(bodyStr, "Authentication Complete") {
		t.Errorf("body %q does not contain 'Authentication Complete'", bodyStr)
	}

	// The session must have been completed — Consume should return the mock token.
	token, ok := s.authSessions.Consume(sess.ID)
	if !ok {
		t.Fatal("Consume returned false; session was not completed")
	}
	if token != "mock-id-token-value" {
		t.Errorf("token = %q, want mock-id-token-value", token)
	}
}

func TestHandleAuthCallback_POST(t *testing.T) {
	s := newTestAuthServer(t)

	sess := s.authSessions.Create("test", "test-code-verifier-post")

	form := url.Values{
		"code":  {"post-auth-code"},
		"state": {sess.ID},
	}
	r := httptest.NewRequest(http.MethodPost, "/api/auth/callback", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	s.HandleAuthCallback(w, r)

	body, _ := io.ReadAll(w.Result().Body)
	bodyStr := string(body)

	if !strings.Contains(bodyStr, "Authentication Complete") {
		t.Errorf("body %q does not contain 'Authentication Complete'", bodyStr)
	}
}

// --- HandleAuthPoll ---

func TestHandleAuthPoll_Pending(t *testing.T) {
	s := newTestAuthServer(t)

	sess := s.authSessions.Create("test", "verifier")

	r := httptest.NewRequest(http.MethodGet, "/api/auth/poll?session="+sess.ID, nil)
	w := httptest.NewRecorder()

	s.HandleAuthPoll(w, r)

	resp := w.Result()
	if resp.Header.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", resp.Header.Get("Content-Type"))
	}

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if result["status"] != "pending" {
		t.Errorf("status = %q, want pending", result["status"])
	}
	if _, hasToken := result["id_token"]; hasToken {
		t.Error("id_token should not be present in a pending response")
	}
}

func TestHandleAuthPoll_Complete(t *testing.T) {
	s := newTestAuthServer(t)

	sess := s.authSessions.Create("test", "verifier")
	s.authSessions.Complete(sess.ID, "completed-id-token")

	r := httptest.NewRequest(http.MethodGet, "/api/auth/poll?session="+sess.ID, nil)
	w := httptest.NewRecorder()

	s.HandleAuthPoll(w, r)

	resp := w.Result()
	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if result["status"] != "complete" {
		t.Errorf("status = %q, want complete", result["status"])
	}
	if result["id_token"] != "completed-id-token" {
		t.Errorf("id_token = %q, want completed-id-token", result["id_token"])
	}
}

// --- renderAuthResult ---

func TestRenderAuthResult_XSS(t *testing.T) {
	s := newTestAuthServer(t)

	malicious := `<script>alert(1)</script>`
	w := httptest.NewRecorder()

	s.renderAuthResult(w, false, malicious)

	body := w.Body.String()

	// The raw script tag must NOT appear in the output.
	if strings.Contains(body, "<script>") {
		t.Error("body contains unescaped <script> tag; XSS not prevented")
	}

	// The escaped version must appear instead.
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Errorf("body %q does not contain escaped &lt;script&gt;", body)
	}
}
