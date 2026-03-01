# Apple ID Auth + Relay-Mediated Browser Login — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add Apple ID as a third OIDC provider and introduce relay-mediated browser-based authentication for all providers.

**Architecture:** The relay server gets four new HTTP endpoints (`/api/auth/login`, `/api/auth/authorize`, `/api/auth/callback`, `/api/auth/poll`) that implement an OAuth2 authorization code flow with PKCE on behalf of CLI and web clients. Apple requires a dynamically-generated JWT client secret signed with a P-256 key. The CLI default login becomes relay-mediated (open browser, poll for token), with device code flow preserved behind `--device-code` for Microsoft/Google.

**Tech Stack:** Go stdlib `net/http`, `crypto/ecdsa`, `go-jose/go-jose/v4` (already indirect dep) for JWT, `github.com/matoous/go-nanoid/v2` for session IDs, `oidc-client-ts` on web side.

**Design doc:** `docs/plans/2026-02-28-apple-id-auth-design.md`

---

## Task 1: Apple Client Secret JWT Generator

**Files:**
- Create: `internal/auth/apple.go`
- Test: `internal/auth/apple_test.go`

**Step 1: Write the failing test**

Create `internal/auth/apple_test.go`:

```go
package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

func TestGenerateAppleClientSecret(t *testing.T) {
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	secret, err := GenerateAppleClientSecret("TEAMID123", "com.example.app", "KEYID456", privKey)
	if err != nil {
		t.Fatalf("GenerateAppleClientSecret: %v", err)
	}

	// Parse and verify the JWT
	tok, err := jwt.ParseSigned(secret, []jose.SignatureAlgorithm{jose.ES256})
	if err != nil {
		t.Fatalf("parse JWT: %v", err)
	}

	var claims jwt.Claims
	if err := tok.Claims(&privKey.PublicKey, &claims); err != nil {
		t.Fatalf("verify claims: %v", err)
	}

	if claims.Issuer != "TEAMID123" {
		t.Errorf("issuer = %q, want TEAMID123", claims.Issuer)
	}
	if claims.Subject != "com.example.app" {
		t.Errorf("subject = %q, want com.example.app", claims.Subject)
	}
	if len(claims.Audience) != 1 || claims.Audience[0] != "https://appleid.apple.com" {
		t.Errorf("audience = %v, want [https://appleid.apple.com]", claims.Audience)
	}
	if claims.Expiry == nil {
		t.Fatal("expiry is nil")
	}
	// Expiry should be ~6 months from now
	expiry := claims.Expiry.Time()
	if expiry.Before(time.Now().Add(179 * 24 * time.Hour)) {
		t.Errorf("expiry %v is too soon", expiry)
	}

	// Verify the header has the kid
	parts := strings.SplitN(secret, ".", 3)
	headerJSON, _ := jose.JSONWebKey{}.MarshalJSON() // just for import
	_ = headerJSON
	// Decode header manually
	rawHeader, err := jose.RawBase64URLDecode(parts[0])
	if err != nil {
		t.Fatalf("decode header: %v", err)
	}
	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if err := json.Unmarshal(rawHeader, &header); err != nil {
		t.Fatalf("unmarshal header: %v", err)
	}
	if header.Kid != "KEYID456" {
		t.Errorf("kid = %q, want KEYID456", header.Kid)
	}
	if header.Alg != "ES256" {
		t.Errorf("alg = %q, want ES256", header.Alg)
	}
}

func TestParseP8PrivateKey(t *testing.T) {
	// Generate a key and encode it as PKCS8 PEM
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	// Marshal to PKCS8
	pkcs8Bytes, err := x509MarshalPKCS8(privKey)
	if err != nil {
		t.Fatal(err)
	}

	pem := encodePEM(pkcs8Bytes, "PRIVATE KEY")
	parsed, err := ParseP8PrivateKey(pem)
	if err != nil {
		t.Fatalf("ParseP8PrivateKey: %v", err)
	}

	if !parsed.Equal(privKey) {
		t.Error("parsed key does not match original")
	}
}
```

Note: The test uses helpers `x509MarshalPKCS8` and `encodePEM` — these are wrappers for stdlib that we'll create in the implementation. Actually, let's simplify: the test should use stdlib directly. Revise as below.

```go
package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

func TestGenerateAppleClientSecret(t *testing.T) {
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	secret, err := GenerateAppleClientSecret("TEAMID123", "com.example.app", "KEYID456", privKey)
	if err != nil {
		t.Fatalf("GenerateAppleClientSecret: %v", err)
	}

	// Parse and verify the JWT
	tok, err := jwt.ParseSigned(secret, []jose.SignatureAlgorithm{jose.ES256})
	if err != nil {
		t.Fatalf("parse JWT: %v", err)
	}

	var claims jwt.Claims
	if err := tok.Claims(&privKey.PublicKey, &claims); err != nil {
		t.Fatalf("verify claims: %v", err)
	}

	if claims.Issuer != "TEAMID123" {
		t.Errorf("issuer = %q, want TEAMID123", claims.Issuer)
	}
	if claims.Subject != "com.example.app" {
		t.Errorf("subject = %q, want com.example.app", claims.Subject)
	}
	if len(claims.Audience) != 1 || claims.Audience[0] != "https://appleid.apple.com" {
		t.Errorf("audience = %v, want [https://appleid.apple.com]", claims.Audience)
	}
	if claims.Expiry == nil {
		t.Fatal("expiry is nil")
	}
	expiry := claims.Expiry.Time()
	if expiry.Before(time.Now().Add(179 * 24 * time.Hour)) {
		t.Errorf("expiry %v is too soon", expiry)
	}

	// Verify header has kid
	rawHeader, _ := jose.RawBase64URLDecode(strings.SplitN(secret, ".", 3)[0])
	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	json.Unmarshal(rawHeader, &header)
	if header.Kid != "KEYID456" {
		t.Errorf("kid = %q, want KEYID456", header.Kid)
	}
	if header.Alg != "ES256" {
		t.Errorf("alg = %q, want ES256", header.Alg)
	}
}

func TestParseP8PrivateKey(t *testing.T) {
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	der, err := x509.MarshalPKCS8PrivateKey(privKey)
	if err != nil {
		t.Fatal(err)
	}

	pemData := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	parsed, err := ParseP8PrivateKey(pemData)
	if err != nil {
		t.Fatalf("ParseP8PrivateKey: %v", err)
	}

	if !parsed.Equal(privKey) {
		t.Error("parsed key does not match original")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/auth/ -run "TestGenerateAppleClientSecret|TestParseP8PrivateKey" -v`
Expected: FAIL — `GenerateAppleClientSecret` and `ParseP8PrivateKey` undefined.

**Step 3: Implement `internal/auth/apple.go`**

```go
package auth

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

// GenerateAppleClientSecret creates a signed JWT for use as Apple's client_secret.
// Apple requires this instead of a static client secret.
// See: https://developer.apple.com/documentation/sign_in_with_apple/generate_and_validate_tokens
func GenerateAppleClientSecret(teamID, clientID, keyID string, privateKey *ecdsa.PrivateKey) (string, error) {
	signerKey := jose.SigningKey{Algorithm: jose.ES256, Key: privateKey}
	signer, err := jose.NewSigner(signerKey, (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", keyID))
	if err != nil {
		return "", fmt.Errorf("create signer: %w", err)
	}

	now := time.Now()
	claims := jwt.Claims{
		Issuer:    teamID,
		Subject:   clientID,
		Audience:  jwt.Audience{"https://appleid.apple.com"},
		IssuedAt:  jwt.NewNumericDate(now),
		Expiry:    jwt.NewNumericDate(now.Add(180 * 24 * time.Hour)), // 6 months
	}

	token, err := jwt.Signed(signer).Claims(claims).Serialize()
	if err != nil {
		return "", fmt.Errorf("sign token: %w", err)
	}
	return token, nil
}

// ParseP8PrivateKey parses an Apple .p8 private key file (PKCS#8 PEM).
func ParseP8PrivateKey(pemData []byte) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode(pemData)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found")
	}

	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse PKCS8 key: %w", err)
	}

	ecKey, ok := key.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("key is not ECDSA (got %T)", key)
	}
	return ecKey, nil
}
```

**Step 4: Promote `go-jose` to direct dependency**

Run: `go get github.com/go-jose/go-jose/v4` then `go mod tidy`

**Step 5: Run tests to verify they pass**

Run: `go test ./internal/auth/ -run "TestGenerateAppleClientSecret|TestParseP8PrivateKey" -v`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/auth/apple.go internal/auth/apple_test.go go.mod go.sum
git commit -m "feat(auth): add Apple client secret JWT generator and P8 key parser"
```

---

## Task 2: Extend ProviderConfig for Apple fields

**Files:**
- Modify: `internal/auth/oidc.go` (lines 13-20, ProviderConfig struct)

**Step 1: Add Apple-specific fields to ProviderConfig**

In `internal/auth/oidc.go`, add three fields to the `ProviderConfig` struct:

```go
type ProviderConfig struct {
	Name          string
	Issuer        string
	ClientID      string
	ClientSecret  string
	DeviceAuthURL string
	// Apple-specific fields
	TeamID     string            // Apple Developer Team ID
	KeyID      string            // Apple key ID
	PrivateKey *ecdsa.PrivateKey // Apple P8 signing key
}
```

Add `"crypto/ecdsa"` to imports.

**Step 2: Add `AuthorizeURL` field to ProviderConfig**

The relay-mediated flow needs to know each provider's authorization endpoint. The `go-oidc` provider already discovers this. Add a helper method to retrieve it:

In `internal/auth/oidc.go`, add a method:

```go
// GetAuthEndpoint returns the authorization endpoint for a named provider.
func (v *Verifier) GetAuthEndpoint(name string) (string, bool) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	entry, ok := v.providers[name]
	if !ok {
		return "", false
	}
	// go-oidc Provider exposes the endpoint via its claims
	var claims struct {
		AuthURL  string `json:"authorization_endpoint"`
		TokenURL string `json:"token_endpoint"`
	}
	if err := entry.provider.Claims(&claims); err != nil {
		return "", false
	}
	return claims.AuthURL, true
}

// GetTokenEndpoint returns the token endpoint for a named provider.
func (v *Verifier) GetTokenEndpoint(name string) (string, bool) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	entry, ok := v.providers[name]
	if !ok {
		return "", false
	}
	var claims struct {
		TokenURL string `json:"token_endpoint"`
	}
	if err := entry.provider.Claims(&claims); err != nil {
		return "", false
	}
	return claims.TokenURL, true
}
```

**Step 3: Verify it compiles**

Run: `go build ./internal/auth/`
Expected: success

**Step 4: Commit**

```bash
git add internal/auth/oidc.go
git commit -m "feat(auth): add Apple fields to ProviderConfig and endpoint helpers"
```

---

## Task 3: Auth Session Store

**Files:**
- Create: `internal/relay/authsession.go`
- Test: `internal/relay/authsession_test.go`

**Step 1: Write the failing test**

Create `internal/relay/authsession_test.go`:

```go
package relay

import (
	"testing"
	"time"
)

func TestAuthSessionStore_CreateAndGet(t *testing.T) {
	store := NewAuthSessionStore(5 * time.Minute)
	defer store.Stop()

	sess := store.Create("google", "test-verifier")
	if sess.ID == "" {
		t.Fatal("session ID is empty")
	}
	if sess.Provider != "google" {
		t.Errorf("provider = %q, want google", sess.Provider)
	}
	if sess.CodeVerifier != "test-verifier" {
		t.Errorf("code_verifier = %q, want test-verifier", sess.CodeVerifier)
	}

	got, ok := store.Get(sess.ID)
	if !ok {
		t.Fatal("session not found")
	}
	if got.ID != sess.ID {
		t.Errorf("got ID %q, want %q", got.ID, sess.ID)
	}
}

func TestAuthSessionStore_Complete(t *testing.T) {
	store := NewAuthSessionStore(5 * time.Minute)
	defer store.Stop()

	sess := store.Create("apple", "verifier")
	store.Complete(sess.ID, "the-id-token")

	got, ok := store.Get(sess.ID)
	if !ok {
		t.Fatal("session not found after complete")
	}
	if got.IDToken != "the-id-token" {
		t.Errorf("id_token = %q, want the-id-token", got.IDToken)
	}
}

func TestAuthSessionStore_Consume(t *testing.T) {
	store := NewAuthSessionStore(5 * time.Minute)
	defer store.Stop()

	sess := store.Create("microsoft", "verifier")
	store.Complete(sess.ID, "token-value")

	token, ok := store.Consume(sess.ID)
	if !ok {
		t.Fatal("consume returned false")
	}
	if token != "token-value" {
		t.Errorf("token = %q, want token-value", token)
	}

	// Second consume should fail (single-use)
	_, ok = store.Consume(sess.ID)
	if ok {
		t.Error("second consume should return false")
	}
}

func TestAuthSessionStore_Expiry(t *testing.T) {
	store := NewAuthSessionStore(50 * time.Millisecond)
	defer store.Stop()

	sess := store.Create("google", "verifier")
	time.Sleep(100 * time.Millisecond)

	_, ok := store.Get(sess.ID)
	if ok {
		t.Error("session should have expired")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/relay/ -run "TestAuthSessionStore" -v`
Expected: FAIL — `NewAuthSessionStore` undefined.

**Step 3: Implement `internal/relay/authsession.go`**

```go
package relay

import (
	"sync"
	"time"

	nanoid "github.com/matoous/go-nanoid/v2"
)

// AuthSession represents a pending browser-based auth flow.
type AuthSession struct {
	ID           string
	Provider     string
	CodeVerifier string
	CreatedAt    time.Time
	IDToken      string // populated when the callback completes
}

// AuthSessionStore manages pending auth sessions with TTL.
type AuthSessionStore struct {
	mu       sync.Mutex
	sessions map[string]*AuthSession
	ttl      time.Duration
	stopCh   chan struct{}
}

// NewAuthSessionStore creates a store with background cleanup.
func NewAuthSessionStore(ttl time.Duration) *AuthSessionStore {
	s := &AuthSessionStore{
		sessions: make(map[string]*AuthSession),
		ttl:      ttl,
		stopCh:   make(chan struct{}),
	}
	go s.cleanup()
	return s
}

// Create starts a new pending auth session.
func (s *AuthSessionStore) Create(provider, codeVerifier string) *AuthSession {
	id, _ := nanoid.New()
	sess := &AuthSession{
		ID:           id,
		Provider:     provider,
		CodeVerifier: codeVerifier,
		CreatedAt:    time.Now(),
	}
	s.mu.Lock()
	s.sessions[id] = sess
	s.mu.Unlock()
	return sess
}

// Get returns a session if it exists and hasn't expired.
func (s *AuthSessionStore) Get(id string) (*AuthSession, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if !ok {
		return nil, false
	}
	if time.Since(sess.CreatedAt) > s.ttl {
		delete(s.sessions, id)
		return nil, false
	}
	return sess, true
}

// Complete sets the ID token on a session.
func (s *AuthSessionStore) Complete(id, idToken string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[id]; ok {
		sess.IDToken = idToken
	}
}

// Consume returns the ID token and deletes the session (single-use).
func (s *AuthSessionStore) Consume(id string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if !ok || sess.IDToken == "" {
		return "", false
	}
	token := sess.IDToken
	delete(s.sessions, id)
	return token, true
}

// Stop shuts down the cleanup goroutine.
func (s *AuthSessionStore) Stop() {
	close(s.stopCh)
}

func (s *AuthSessionStore) cleanup() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.mu.Lock()
			now := time.Now()
			for id, sess := range s.sessions {
				if now.Sub(sess.CreatedAt) > s.ttl {
					delete(s.sessions, id)
				}
			}
			s.mu.Unlock()
		}
	}
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/relay/ -run "TestAuthSessionStore" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/relay/authsession.go internal/relay/authsession_test.go
git commit -m "feat(relay): add auth session store with TTL and single-use consume"
```

---

## Task 4: Relay Auth Flow HTTP Handlers

**Files:**
- Create: `internal/relay/handler_auth.go`
- Modify: `internal/relay/server.go` (add `authSessions` field and routes)

**Step 1: Add `authSessions` field to Server**

In `internal/relay/server.go`:

```go
type Server struct {
	hub          *Hub
	logger       *slog.Logger
	baseURL      string
	verifier     *auth.Verifier
	devMode      bool
	authSessions *AuthSessionStore
}
```

Update `NewServer` to accept and store it:

```go
func NewServer(hub *Hub, logger *slog.Logger, baseURL string, verifier *auth.Verifier, devMode bool) *Server {
	return &Server{
		hub:          hub,
		logger:       logger,
		baseURL:      baseURL,
		verifier:     verifier,
		devMode:      devMode,
		authSessions: NewAuthSessionStore(5 * time.Minute),
	}
}
```

Add imports: `"time"`.

Register routes in `Handler()`:

```go
// Auth flow endpoints (no auth middleware — these *are* the auth mechanism)
mux.HandleFunc("POST /api/auth/login", s.HandleAuthLogin)
mux.HandleFunc("GET /api/auth/authorize", s.HandleAuthAuthorize)
mux.HandleFunc("GET /api/auth/callback", s.HandleAuthCallback)
mux.HandleFunc("POST /api/auth/callback", s.HandleAuthCallback) // Apple uses POST
mux.HandleFunc("GET /api/auth/poll", s.HandleAuthPoll)
```

Add these routes before the static file catch-all.

**Step 2: Implement `internal/relay/handler_auth.go`**

```go
package relay

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
)

// --- PKCE helpers ---

func generateCodeVerifier() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func codeChallenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// --- Handlers ---

type authLoginRequest struct {
	Provider string `json:"provider"`
}

type authLoginResponse struct {
	SessionID string `json:"session_id"`
	AuthURL   string `json:"auth_url"`
}

// HandleAuthLogin starts a relay-mediated browser auth flow.
// POST /api/auth/login  body: {"provider":"apple"|"microsoft"|"google"}
func (s *Server) HandleAuthLogin(w http.ResponseWriter, r *http.Request) {
	var req authLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	// Verify the provider is registered
	if _, ok := s.verifier.GetProvider(req.Provider); !ok {
		http.Error(w, `{"error":"unknown provider"}`, http.StatusBadRequest)
		return
	}

	verifier := generateCodeVerifier()
	sess := s.authSessions.Create(req.Provider, verifier)

	authURL := fmt.Sprintf("%s/api/auth/authorize?session=%s", s.baseURL, sess.ID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(authLoginResponse{
		SessionID: sess.ID,
		AuthURL:   authURL,
	})
}

// HandleAuthAuthorize redirects the browser to the OIDC provider's authorize endpoint.
// GET /api/auth/authorize?session=SESSION_ID
func (s *Server) HandleAuthAuthorize(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session")
	sess, ok := s.authSessions.Get(sessionID)
	if !ok {
		http.Error(w, "invalid or expired session", http.StatusBadRequest)
		return
	}

	authEndpoint, ok := s.verifier.GetAuthEndpoint(sess.Provider)
	if !ok {
		http.Error(w, "provider auth endpoint not found", http.StatusInternalServerError)
		return
	}

	cfg, _ := s.verifier.GetProvider(sess.Provider)
	redirectURI := fmt.Sprintf("%s/api/auth/callback", s.baseURL)

	params := url.Values{
		"client_id":             {cfg.ClientID},
		"redirect_uri":          {redirectURI},
		"response_type":         {"code"},
		"scope":                 {"openid email profile"},
		"state":                 {sessionID},
		"code_challenge":        {codeChallenge(sess.CodeVerifier)},
		"code_challenge_method": {"S256"},
	}

	// Apple-specific: use response_mode=form_post
	if sess.Provider == "apple" {
		params.Set("response_mode", "form_post")
	}

	target := authEndpoint + "?" + params.Encode()
	http.Redirect(w, r, target, http.StatusFound)
}

// HandleAuthCallback handles the OIDC provider's redirect back.
// GET or POST /api/auth/callback  (Apple uses POST with form_post)
func (s *Server) HandleAuthCallback(w http.ResponseWriter, r *http.Request) {
	var code, state string

	if r.Method == http.MethodPost {
		// Apple form_post
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form data", http.StatusBadRequest)
			return
		}
		code = r.FormValue("code")
		state = r.FormValue("state")
	} else {
		code = r.URL.Query().Get("code")
		state = r.URL.Query().Get("state")
	}

	if code == "" || state == "" {
		// Check for error response from provider
		errMsg := r.URL.Query().Get("error_description")
		if errMsg == "" {
			errMsg = r.URL.Query().Get("error")
		}
		if errMsg == "" {
			errMsg = "missing code or state"
		}
		s.renderAuthResult(w, false, errMsg)
		return
	}

	sess, ok := s.authSessions.Get(state)
	if !ok {
		s.renderAuthResult(w, false, "session expired or invalid")
		return
	}

	cfg, _ := s.verifier.GetProvider(sess.Provider)
	tokenEndpoint, ok := s.verifier.GetTokenEndpoint(sess.Provider)
	if !ok {
		s.renderAuthResult(w, false, "provider token endpoint not found")
		return
	}

	// Exchange authorization code for tokens
	redirectURI := fmt.Sprintf("%s/api/auth/callback", s.baseURL)
	data := url.Values{
		"client_id":     {cfg.ClientID},
		"code":          {code},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {redirectURI},
		"code_verifier": {sess.CodeVerifier},
	}

	// Add client_secret — for Apple, generate it dynamically
	if sess.Provider == "apple" && cfg.PrivateKey != nil {
		secret, err := generateAppleSecret(cfg)
		if err != nil {
			s.logger.Error("generate Apple client secret", slog.String("err", err.Error()))
			s.renderAuthResult(w, false, "internal error")
			return
		}
		data.Set("client_secret", secret)
	} else if cfg.ClientSecret != "" {
		data.Set("client_secret", cfg.ClientSecret)
	}

	tokenResp, err := http.Post(tokenEndpoint, "application/x-www-form-urlencoded", strings.NewReader(data.Encode()))
	if err != nil {
		s.logger.Error("token exchange request", slog.String("err", err.Error()))
		s.renderAuthResult(w, false, "token exchange failed")
		return
	}
	defer tokenResp.Body.Close()

	body, _ := io.ReadAll(tokenResp.Body)
	var tokenResult struct {
		IDToken string `json:"id_token"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(body, &tokenResult); err != nil {
		s.renderAuthResult(w, false, "invalid token response")
		return
	}

	if tokenResult.Error != "" {
		s.renderAuthResult(w, false, "token error: "+tokenResult.Error)
		return
	}

	if tokenResult.IDToken == "" {
		s.renderAuthResult(w, false, "no id_token in response")
		return
	}

	s.authSessions.Complete(state, tokenResult.IDToken)
	s.renderAuthResult(w, true, "")
}

// HandleAuthPoll checks if a login session has completed.
// GET /api/auth/poll?session=SESSION_ID
func (s *Server) HandleAuthPoll(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session")

	token, ok := s.authSessions.Consume(sessionID)
	w.Header().Set("Content-Type", "application/json")
	if ok {
		json.NewEncoder(w).Encode(map[string]string{"status": "complete", "id_token": token})
	} else {
		// Could be pending or not found — either way, not ready
		json.NewEncoder(w).Encode(map[string]string{"status": "pending"})
	}
}

func (s *Server) renderAuthResult(w http.ResponseWriter, success bool, errMsg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if success {
		fmt.Fprint(w, `<!DOCTYPE html><html><body style="background:#0a0a0a;color:#00ff41;font-family:monospace;display:flex;justify-content:center;align-items:center;height:100vh;margin:0"><div style="text-align:center"><h2>Authentication Complete</h2><p>You can close this tab and return to your terminal.</p></div></body></html>`)
	} else {
		fmt.Fprintf(w, `<!DOCTYPE html><html><body style="background:#0a0a0a;color:#ff4444;font-family:monospace;display:flex;justify-content:center;align-items:center;height:100vh;margin:0"><div style="text-align:center"><h2>Authentication Failed</h2><p>%s</p></div></body></html>`, errMsg)
	}
}

// generateAppleSecret wraps auth.GenerateAppleClientSecret using the provider config.
func generateAppleSecret(cfg auth.ProviderConfig) (string, error) {
	return auth.GenerateAppleClientSecret(cfg.TeamID, cfg.ClientID, cfg.KeyID, cfg.PrivateKey)
}
```

Note: Import `"github.com/brporter/phosphor/internal/auth"` is needed — it's already imported in other relay package files but this file needs its own import.

**Step 3: Verify it compiles**

Run: `go build ./internal/relay/`
Expected: success (imports `auth` package which has the new `GenerateAppleClientSecret`)

**Step 4: Commit**

```bash
git add internal/relay/handler_auth.go internal/relay/server.go
git commit -m "feat(relay): add relay-mediated auth flow endpoints (/api/auth/*)"
```

---

## Task 5: Register Apple Provider in Relay Startup

**Files:**
- Modify: `cmd/relay/main.go` (add Apple provider registration block)

**Step 1: Add Apple provider registration**

After the Google provider block in `cmd/relay/main.go`, add:

```go
if clientID := os.Getenv("APPLE_CLIENT_ID"); clientID != "" {
	teamID := os.Getenv("APPLE_TEAM_ID")
	keyID := os.Getenv("APPLE_KEY_ID")
	pkRaw := os.Getenv("APPLE_PRIVATE_KEY")

	if teamID == "" || keyID == "" || pkRaw == "" {
		logger.Warn("APPLE_CLIENT_ID set but missing APPLE_TEAM_ID, APPLE_KEY_ID, or APPLE_PRIVATE_KEY")
	} else {
		privateKey, err := auth.ParseP8PrivateKey([]byte(pkRaw))
		if err != nil {
			logger.Warn("failed to parse Apple private key", "err", err)
		} else {
			if err := verifier.AddProvider(ctx, auth.ProviderConfig{
				Name:       "apple",
				Issuer:     "https://appleid.apple.com",
				ClientID:   clientID,
				TeamID:     teamID,
				KeyID:      keyID,
				PrivateKey: privateKey,
			}); err != nil {
				logger.Warn("failed to register Apple provider", "err", err)
			}
		}
	}
}
```

**Step 2: Verify it compiles**

Run: `go build ./cmd/relay/`
Expected: success

**Step 3: Commit**

```bash
git add cmd/relay/main.go
git commit -m "feat(relay): register Apple OIDC provider from env vars at startup"
```

---

## Task 6: CLI Relay-Mediated Browser Login

**Files:**
- Create: `internal/cli/browser.go`
- Modify: `internal/cli/login.go`
- Modify: `cmd/phosphor/main.go`

**Step 1: Create `internal/cli/browser.go`**

This file handles the relay-mediated browser flow used by all providers:

```go
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

type loginStartResponse struct {
	SessionID string `json:"session_id"`
	AuthURL   string `json:"auth_url"`
}

type pollResponse struct {
	Status  string `json:"status"`
	IDToken string `json:"id_token"`
}

// BrowserLogin performs relay-mediated browser-based authentication.
func BrowserLogin(ctx context.Context, relayURL, provider string) (string, error) {
	// Convert ws:// to http:// for REST calls
	httpBase := relayURL
	httpBase = strings.Replace(httpBase, "ws://", "http://", 1)
	httpBase = strings.Replace(httpBase, "wss://", "https://", 1)

	// Step 1: Start auth session on relay
	body := fmt.Sprintf(`{"provider":%q}`, provider)
	resp, err := http.Post(httpBase+"/api/auth/login", "application/json", strings.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("start auth session: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("relay returned %d starting auth", resp.StatusCode)
	}

	var loginResp loginStartResponse
	if err := json.NewDecoder(resp.Body).Decode(&loginResp); err != nil {
		return "", fmt.Errorf("decode login response: %w", err)
	}

	// Step 2: Open browser
	fmt.Fprintf(os.Stderr, "\nOpening browser for authentication...\n")
	fmt.Fprintf(os.Stderr, "If the browser doesn't open, visit: %s\n\n", loginResp.AuthURL)
	openBrowser(loginResp.AuthURL)

	// Step 3: Poll for completion
	fmt.Fprintf(os.Stderr, "Waiting for authentication...\n")
	pollURL := fmt.Sprintf("%s/api/auth/poll?session=%s", httpBase, loginResp.SessionID)

	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(2 * time.Second):
		}

		pollResp, err := http.Get(pollURL)
		if err != nil {
			continue
		}

		var pr pollResponse
		json.NewDecoder(pollResp.Body).Decode(&pr)
		pollResp.Body.Close()

		if pr.Status == "complete" && pr.IDToken != "" {
			return pr.IDToken, nil
		}
	}

	return "", fmt.Errorf("authentication timed out — please try again")
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	cmd.Start()
}
```

**Step 2: Update `internal/cli/login.go`**

Replace the `Login` function to use relay-mediated flow by default, with device code as fallback:

```go
package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/brporter/phosphor/internal/auth"
)

// Provider configs for device code flow (Microsoft and Google only).
var deviceCodeConfigs = map[string]struct {
	DeviceAuthURL string
	TokenURL      string
	ClientIDEnv   string
	Scopes        []string
}{
	"microsoft": {
		DeviceAuthURL: "https://login.microsoftonline.com/common/oauth2/v2.0/devicecode",
		TokenURL:      "https://login.microsoftonline.com/common/oauth2/v2.0/token",
		ClientIDEnv:   "PHOSPHOR_MICROSOFT_CLIENT_ID",
		Scopes:        []string{"openid", "profile", "email", "offline_access"},
	},
	"google": {
		DeviceAuthURL: "https://oauth2.googleapis.com/device/code",
		TokenURL:      "https://oauth2.googleapis.com/token",
		ClientIDEnv:   "PHOSPHOR_GOOGLE_CLIENT_ID",
		Scopes:        []string{"openid", "profile", "email"},
	},
}

// supportedProviders lists all providers that support relay-mediated login.
var supportedProviders = []string{"apple", "microsoft", "google"}

// Login performs authentication for the given provider.
// If useDeviceCode is true and the provider supports it, uses device code flow.
// Otherwise uses relay-mediated browser flow.
func Login(ctx context.Context, providerName, relayURL string, useDeviceCode bool) error {
	providerName = strings.ToLower(providerName)

	// Validate provider name
	valid := false
	for _, p := range supportedProviders {
		if p == providerName {
			valid = true
			break
		}
	}
	if !valid {
		return fmt.Errorf("unknown provider: %s (supported: %s)", providerName, strings.Join(supportedProviders, ", "))
	}

	// Device code flow (Microsoft/Google only)
	if useDeviceCode {
		return loginDeviceCode(ctx, providerName)
	}

	// Relay-mediated browser flow (all providers)
	token, err := BrowserLogin(ctx, relayURL, providerName)
	if err != nil {
		return fmt.Errorf("browser login: %w", err)
	}

	if err := SaveTokenCache(&TokenCache{
		AccessToken: token,
		Provider:    providerName,
	}); err != nil {
		return fmt.Errorf("save token: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Authenticated successfully!\n")
	return nil
}

func loginDeviceCode(ctx context.Context, providerName string) error {
	p, ok := deviceCodeConfigs[providerName]
	if !ok {
		return fmt.Errorf("device code flow not supported for %s — use browser login instead", providerName)
	}

	clientID := os.Getenv(p.ClientIDEnv)
	if clientID == "" {
		return fmt.Errorf("no client ID configured — set %s environment variable", p.ClientIDEnv)
	}

	dcr, err := auth.RequestDeviceCode(ctx, p.DeviceAuthURL, clientID, p.Scopes)
	if err != nil {
		return fmt.Errorf("request device code: %w", err)
	}

	fmt.Fprintf(os.Stderr, "\nTo sign in, visit: %s\n", dcr.VerificationURI)
	fmt.Fprintf(os.Stderr, "Enter code: %s\n\n", dcr.UserCode)
	fmt.Fprintf(os.Stderr, "Waiting for authentication...\n")

	dtr, err := auth.PollForToken(ctx, p.TokenURL, clientID, dcr.DeviceCode)
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	token := dtr.IDToken
	if token == "" {
		token = dtr.AccessToken
	}

	if err := SaveTokenCache(&TokenCache{
		AccessToken:  token,
		RefreshToken: dtr.RefreshToken,
		Provider:     providerName,
	}); err != nil {
		return fmt.Errorf("save token: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Authenticated successfully!\n")
	return nil
}
```

**Step 3: Update `cmd/phosphor/main.go`**

Update the login command to pass the relay URL and `--device-code` flag:

```go
var provider string
var useDeviceCode bool
loginCmd := &cobra.Command{
	Use:   "login",
	Short: "Authenticate with an identity provider",
	RunE: func(cmd *cobra.Command, args []string) error {
		relay := relayURL
		if relay == "" {
			relay = cli.DefaultConfig().RelayURL
		}
		return cli.Login(context.Background(), provider, relay, useDeviceCode)
	},
}
loginCmd.Flags().StringVar(&provider, "provider", "microsoft", "OIDC provider (apple, microsoft, google)")
loginCmd.Flags().BoolVar(&useDeviceCode, "device-code", false, "Use device code flow instead of browser (Microsoft/Google only)")
```

**Step 4: Verify it compiles**

Run: `go build ./cmd/phosphor/`
Expected: success

**Step 5: Commit**

```bash
git add internal/cli/browser.go internal/cli/login.go cmd/phosphor/main.go
git commit -m "feat(cli): relay-mediated browser login for all providers, device-code flag for legacy"
```

---

## Task 7: Web SPA — Apple Auth Support

**Files:**
- Modify: `web/src/auth/AuthProvider.tsx`
- Modify: `web/src/auth/AuthCallback.tsx`
- Modify: `web/src/components/Layout.tsx`
- Modify: `web/src/components/ProtectedRoute.tsx`

**Step 1: Add Apple to `AuthProvider.tsx`**

Add Apple to `providerConfigs`:

```typescript
apple: {
  authority: "https://appleid.apple.com",
  client_id_env: "VITE_APPLE_CLIENT_ID",
  scope: "openid name email",
},
```

Update the `login` callback to use relay-mediated flow for Apple:

```typescript
const login = useCallback(async (provider: string) => {
  localStorage.setItem("phosphor_provider", provider);

  // Apple uses relay-mediated flow
  if (provider === "apple") {
    const resp = await fetch("/api/auth/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ provider: "apple" }),
    });
    const { auth_url, session_id } = await resp.json();
    // Store session_id for polling after redirect
    localStorage.setItem("phosphor_auth_session", session_id);
    window.location.href = auth_url;
    return;
  }

  const mgr = createUserManager(provider);
  if (!mgr) {
    // Dev mode fallback
    setUser({
      id_token: "dev:anonymous",
      access_token: "dev:anonymous",
      token_type: "Bearer",
      profile: { sub: "anonymous", iss: "dev" },
      expired: false,
    } as unknown as User);
    return;
  }
  await mgr.signinRedirect();
}, []);
```

Update the `useEffect` session restore to also check for a pending Apple auth session:

```typescript
useEffect(() => {
  // Check for pending Apple auth session
  const appleSession = localStorage.getItem("phosphor_auth_session");
  if (appleSession) {
    localStorage.removeItem("phosphor_auth_session");
    const poll = async () => {
      const resp = await fetch(`/api/auth/poll?session=${appleSession}`);
      const data = await resp.json();
      if (data.status === "complete" && data.id_token) {
        // Parse minimal user info from the id_token (JWT payload)
        const payload = JSON.parse(atob(data.id_token.split(".")[1]));
        setUser({
          id_token: data.id_token,
          access_token: data.id_token,
          token_type: "Bearer",
          profile: { sub: payload.sub, iss: payload.iss, email: payload.email },
          expired: false,
        } as unknown as User);
      }
      setIsLoading(false);
    };
    poll();
    return;
  }

  const provider = localStorage.getItem("phosphor_provider") ?? "microsoft";
  const mgr = createUserManager(provider);
  if (mgr) {
    mgr
      .getUser()
      .then((u) => setUser(u))
      .finally(() => setIsLoading(false));
  } else {
    setIsLoading(false);
  }
}, []);
```

**Step 2: Add Apple to `AuthCallback.tsx`**

Add to the `configs` object:

```typescript
apple: {
  authority: "https://appleid.apple.com",
  client_id_env: "VITE_APPLE_CLIENT_ID",
},
```

Note: For Apple, the callback is handled by the relay (`/api/auth/callback`), so the SPA's `/auth/callback` route won't be hit for Apple. The config entry is for completeness.

**Step 3: Add Apple button to `Layout.tsx`**

In the sign-in buttons section, add after the Google button:

```tsx
<button onClick={() => void login("apple")}>Apple</button>
```

**Step 4: Add Apple button to `ProtectedRoute.tsx`**

In the sign-in buttons section, add after the Google button:

```tsx
<button onClick={() => void login("apple")}>
  sign in with Apple
</button>
```

**Step 5: Verify web builds**

Run: `cd web && npm run build`
Expected: success (tsc + vite build)

**Step 6: Commit**

```bash
git add web/src/auth/AuthProvider.tsx web/src/auth/AuthCallback.tsx web/src/components/Layout.tsx web/src/components/ProtectedRoute.tsx
git commit -m "feat(web): add Apple ID sign-in with relay-mediated auth flow"
```

---

## Task 8: Integration Verification

**Step 1: Verify full Go build**

Run: `go build ./...`
Expected: success

**Step 2: Run all Go tests**

Run: `go test ./... -v`
Expected: all tests pass

**Step 3: Verify web build**

Run: `cd web && npm run build`
Expected: success

**Step 4: Final commit (if any fixups needed)**

Only if previous steps required adjustments.
