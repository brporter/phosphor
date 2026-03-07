# API Key Authentication Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add API key authentication so the phosphor daemon can connect to the relay without a user OIDC token.

**Architecture:** API keys are HS256-signed JWTs issued by the relay, carrying the generating user's identity. The relay validates them via HMAC signature check and a file-based revocation blocklist. The daemon stores the key in its config file and sends it with a `phk:` prefix in the Hello message. The SPA exposes key generation on a new `/settings` page.

**Tech Stack:** Go (go-jose/v4 for JWT), React/TypeScript (SPA), HS256 signing

---

### Task 1: API Key Signing & Verification (internal/relay/apikey.go)

**Files:**
- Create: `internal/relay/apikey.go`
- Create: `internal/relay/apikey_test.go`

**Step 1: Write the tests**

```go
// internal/relay/apikey_test.go
package relay

import (
	"testing"
)

func TestGenerateAPIKey(t *testing.T) {
	secret := []byte("test-secret-32-bytes-long-xxxxx")
	key, keyID, err := GenerateAPIKey(secret, "microsoft", "user@example.com")
	if err != nil {
		t.Fatalf("GenerateAPIKey: %v", err)
	}
	if key == "" {
		t.Fatal("key is empty")
	}
	if keyID == "" {
		t.Fatal("keyID is empty")
	}
}

func TestVerifyAPIKey_Valid(t *testing.T) {
	secret := []byte("test-secret-32-bytes-long-xxxxx")
	key, keyID, _ := GenerateAPIKey(secret, "microsoft", "user@example.com")

	claims, err := VerifyAPIKey(secret, key)
	if err != nil {
		t.Fatalf("VerifyAPIKey: %v", err)
	}
	if claims.Subject != "microsoft:user@example.com" {
		t.Errorf("Subject = %q, want microsoft:user@example.com", claims.Subject)
	}
	if claims.KeyID != keyID {
		t.Errorf("KeyID = %q, want %q", claims.KeyID, keyID)
	}
	if claims.Issuer != "phosphor" {
		t.Errorf("Issuer = %q, want phosphor", claims.Issuer)
	}
}

func TestVerifyAPIKey_WrongSecret(t *testing.T) {
	secret := []byte("test-secret-32-bytes-long-xxxxx")
	key, _, _ := GenerateAPIKey(secret, "microsoft", "user@example.com")

	_, err := VerifyAPIKey([]byte("wrong-secret-32-bytes-long-xxxx"), key)
	if err == nil {
		t.Fatal("expected error for wrong secret")
	}
}

func TestVerifyAPIKey_Malformed(t *testing.T) {
	secret := []byte("test-secret-32-bytes-long-xxxxx")
	_, err := VerifyAPIKey(secret, "not-a-jwt")
	if err == nil {
		t.Fatal("expected error for malformed token")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/relay/ -run TestGenerateAPIKey -v -count=1`
Expected: FAIL — `GenerateAPIKey` undefined

**Step 3: Write the implementation**

```go
// internal/relay/apikey.go
package relay

import (
	"fmt"
	"strings"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	gonanoid "github.com/matoous/go-nanoid/v2"
)

// APIKeyClaims holds the claims embedded in an API key JWT.
type APIKeyClaims struct {
	Subject string
	KeyID   string
	Issuer  string
}

// GenerateAPIKey creates a signed API key JWT for the given user identity.
// Returns (raw JWT string without phk: prefix, key ID, error).
func GenerateAPIKey(secret []byte, provider, sub string) (string, string, error) {
	keyID, err := gonanoid.New(12)
	if err != nil {
		return "", "", fmt.Errorf("generate key id: %w", err)
	}

	sig, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.HS256, Key: secret},
		(&jose.SignerOptions{}).WithHeader("kid", keyID),
	)
	if err != nil {
		return "", "", fmt.Errorf("create signer: %w", err)
	}

	claims := jwt.Claims{
		Subject:  provider + ":" + sub,
		Issuer:   "phosphor",
		ID:       keyID,
		IssuedAt: jwt.NewNumericDate(time.Now()),
	}

	raw, err := jwt.Signed(sig).Claims(claims).Serialize()
	if err != nil {
		return "", "", fmt.Errorf("sign jwt: %w", err)
	}

	return raw, keyID, nil
}

// VerifyAPIKey validates an API key JWT and returns its claims.
// The token parameter is the raw JWT (without phk: prefix).
func VerifyAPIKey(secret []byte, token string) (*APIKeyClaims, error) {
	parsed, err := jwt.ParseSigned(token, []jose.SignatureAlgorithm{jose.HS256})
	if err != nil {
		return nil, fmt.Errorf("parse api key: %w", err)
	}

	var claims jwt.Claims
	if err := parsed.Claims(secret, &claims); err != nil {
		return nil, fmt.Errorf("verify api key: %w", err)
	}

	if claims.Issuer != "phosphor" {
		return nil, fmt.Errorf("invalid issuer: %s", claims.Issuer)
	}

	return &APIKeyClaims{
		Subject: claims.Subject,
		KeyID:   claims.ID,
		Issuer:  claims.Issuer,
	}, nil
}

// ParseAPIKeySubject splits an API key subject "provider:sub" into its parts.
func ParseAPIKeySubject(subject string) (string, string, error) {
	parts := strings.SplitN(subject, ":", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid api key subject: %s", subject)
	}
	return parts[0], parts[1], nil
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/relay/ -run TestGenerateAPIKey -v -count=1`
Run: `go test ./internal/relay/ -run TestVerifyAPIKey -v -count=1`
Expected: PASS

**Step 5: Commit**

```
feat: add API key generation and verification
```

---

### Task 2: Revocation Blocklist (internal/relay/blocklist.go)

**Files:**
- Create: `internal/relay/blocklist.go`
- Create: `internal/relay/blocklist_test.go`

**Step 1: Write the tests**

```go
// internal/relay/blocklist_test.go
package relay

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBlocklist_Empty(t *testing.T) {
	bl := NewBlocklist("")
	defer bl.Stop()
	if bl.IsRevoked("any-key-id") {
		t.Error("empty blocklist should not revoke anything")
	}
}

func TestBlocklist_LoadFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "revoked.txt")
	os.WriteFile(path, []byte("key-1\nkey-2\n\n  \nkey-3\n"), 0644)

	bl := NewBlocklist(path)
	defer bl.Stop()

	if !bl.IsRevoked("key-1") {
		t.Error("key-1 should be revoked")
	}
	if !bl.IsRevoked("key-2") {
		t.Error("key-2 should be revoked")
	}
	if !bl.IsRevoked("key-3") {
		t.Error("key-3 should be revoked")
	}
	if bl.IsRevoked("key-4") {
		t.Error("key-4 should not be revoked")
	}
}

func TestBlocklist_MissingFile(t *testing.T) {
	bl := NewBlocklist("/nonexistent/file.txt")
	defer bl.Stop()
	if bl.IsRevoked("anything") {
		t.Error("missing file should not revoke anything")
	}
}

func TestBlocklist_FileWatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "revoked.txt")
	os.WriteFile(path, []byte("key-1\n"), 0644)

	bl := NewBlocklist(path)
	defer bl.Stop()

	if !bl.IsRevoked("key-1") {
		t.Fatal("key-1 should be revoked initially")
	}
	if bl.IsRevoked("key-2") {
		t.Fatal("key-2 should not be revoked initially")
	}

	// Update the file
	os.WriteFile(path, []byte("key-1\nkey-2\n"), 0644)

	// Wait for fsnotify to pick up the change
	time.Sleep(200 * time.Millisecond)

	if !bl.IsRevoked("key-2") {
		t.Error("key-2 should be revoked after file update")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/relay/ -run TestBlocklist -v -count=1`
Expected: FAIL — `NewBlocklist` undefined

**Step 3: Write the implementation**

```go
// internal/relay/blocklist.go
package relay

import (
	"bufio"
	"os"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
)

// Blocklist tracks revoked API key IDs loaded from a file.
type Blocklist struct {
	path    string
	mu      sync.RWMutex
	revoked map[string]struct{}
	stopCh  chan struct{}
}

// NewBlocklist creates a blocklist that loads from the given file path.
// If path is empty or the file doesn't exist, no keys are revoked.
// The file is watched for changes via fsnotify.
func NewBlocklist(path string) *Blocklist {
	bl := &Blocklist{
		path:    path,
		revoked: make(map[string]struct{}),
		stopCh:  make(chan struct{}),
	}
	bl.load()
	if path != "" {
		go bl.watch()
	}
	return bl
}

// IsRevoked returns true if the given key ID is in the blocklist.
func (bl *Blocklist) IsRevoked(keyID string) bool {
	bl.mu.RLock()
	defer bl.mu.RUnlock()
	_, ok := bl.revoked[keyID]
	return ok
}

// Stop stops the file watcher.
func (bl *Blocklist) Stop() {
	select {
	case <-bl.stopCh:
	default:
		close(bl.stopCh)
	}
}

func (bl *Blocklist) load() {
	if bl.path == "" {
		return
	}
	f, err := os.Open(bl.path)
	if err != nil {
		return
	}
	defer f.Close()

	revoked := make(map[string]struct{})
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			revoked[line] = struct{}{}
		}
	}

	bl.mu.Lock()
	bl.revoked = revoked
	bl.mu.Unlock()
}

func (bl *Blocklist) watch() {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return
	}
	defer watcher.Close()

	if err := watcher.Add(bl.path); err != nil {
		return
	}

	for {
		select {
		case <-bl.stopCh:
			return
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if event.Op&(fsnotify.Write|fsnotify.Create) != 0 {
				bl.load()
			}
		case _, ok := <-watcher.Errors:
			if !ok {
				return
			}
		}
	}
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/relay/ -run TestBlocklist -v -count=1`
Expected: PASS

**Step 5: Commit**

```
feat: add file-based API key revocation blocklist
```

---

### Task 3: Wire API Key Into Server & verifyToken

**Files:**
- Modify: `internal/relay/server.go`
- Modify: `internal/relay/auth.go`
- Modify: `internal/relay/auth_test.go`
- Modify: `cmd/relay/main.go`
- Modify: `.env-template`

**Step 1: Write the tests**

Add to `internal/relay/auth_test.go`:

```go
func TestVerifyToken_APIKey_Valid(t *testing.T) {
	secret := []byte("test-secret-32-bytes-long-xxxxx")
	key, _, _ := GenerateAPIKey(secret, "microsoft", "user@example.com")

	bl := NewBlocklist("")
	defer bl.Stop()
	s := &Server{devMode: false, logger: slog.Default(), apiKeySecret: secret, blocklist: bl}

	provider, sub, err := s.verifyToken(context.Background(), "phk:"+key)
	if err != nil {
		t.Fatalf("verifyToken: %v", err)
	}
	if provider != "microsoft" {
		t.Errorf("provider = %q, want microsoft", provider)
	}
	if sub != "user@example.com" {
		t.Errorf("sub = %q, want user@example.com", sub)
	}
}

func TestVerifyToken_APIKey_Revoked(t *testing.T) {
	secret := []byte("test-secret-32-bytes-long-xxxxx")
	key, keyID, _ := GenerateAPIKey(secret, "microsoft", "user@example.com")

	dir := t.TempDir()
	path := filepath.Join(dir, "revoked.txt")
	os.WriteFile(path, []byte(keyID+"\n"), 0644)
	bl := NewBlocklist(path)
	defer bl.Stop()

	s := &Server{devMode: false, logger: slog.Default(), apiKeySecret: secret, blocklist: bl}

	_, _, err := s.verifyToken(context.Background(), "phk:"+key)
	if err == nil {
		t.Fatal("expected error for revoked key")
	}
}

func TestVerifyToken_APIKey_DevMode(t *testing.T) {
	bl := NewBlocklist("")
	defer bl.Stop()
	s := &Server{devMode: true, logger: slog.Default(), blocklist: bl}

	provider, sub, err := s.verifyToken(context.Background(), "phk:anything")
	if err != nil {
		t.Fatalf("verifyToken: %v", err)
	}
	if provider != "dev" {
		t.Errorf("provider = %q, want dev", provider)
	}
	if sub != "anonymous" {
		t.Errorf("sub = %q, want anonymous", sub)
	}
}

func TestVerifyToken_APIKey_WrongSecret(t *testing.T) {
	secret := []byte("test-secret-32-bytes-long-xxxxx")
	key, _, _ := GenerateAPIKey(secret, "microsoft", "user@example.com")

	bl := NewBlocklist("")
	defer bl.Stop()
	s := &Server{devMode: false, logger: slog.Default(), apiKeySecret: []byte("wrong-secret-xxxxxxxxxxxxx-xxxx"), blocklist: bl}

	_, _, err := s.verifyToken(context.Background(), "phk:"+key)
	if err == nil {
		t.Fatal("expected error for wrong secret")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/relay/ -run TestVerifyToken_APIKey -v -count=1`
Expected: FAIL — `apiKeySecret` field unknown

**Step 3: Modify the Server struct**

In `internal/relay/server.go`, add `apiKeySecret` and `blocklist` fields to the `Server` struct and update `NewServer`:

```go
type Server struct {
	hub          *Hub
	logger       *slog.Logger
	baseURL      string
	verifier     *auth.Verifier
	devMode      bool
	authSessions AuthSessionStoreI
	apiKeySecret []byte
	blocklist    *Blocklist
}

func NewServer(hub *Hub, logger *slog.Logger, baseURL string, verifier *auth.Verifier, devMode bool, authSessions AuthSessionStoreI, apiKeySecret []byte, blocklist *Blocklist) *Server {
	return &Server{hub: hub, logger: logger, baseURL: baseURL, verifier: verifier, devMode: devMode, authSessions: authSessions, apiKeySecret: apiKeySecret, blocklist: blocklist}
}
```

**Step 4: Update verifyToken in auth.go**

Add the `phk:` branch to `verifyToken` — insert before the OIDC verification block:

```go
func (s *Server) verifyToken(ctx context.Context, token string) (string, string, error) {
	if token == "" && s.devMode {
		return "dev", "anonymous", nil
	}

	// API key authentication
	if strings.HasPrefix(token, "phk:") {
		raw := strings.TrimPrefix(token, "phk:")
		if s.devMode {
			return "dev", "anonymous", nil
		}
		claims, err := VerifyAPIKey(s.apiKeySecret, raw)
		if err != nil {
			return "", "", fmt.Errorf("invalid api key: %w", err)
		}
		if s.blocklist != nil && s.blocklist.IsRevoked(claims.KeyID) {
			return "", "", fmt.Errorf("api key revoked")
		}
		provider, sub, err := ParseAPIKeySubject(claims.Subject)
		if err != nil {
			return "", "", err
		}
		return provider, sub, nil
	}

	// Dev-mode fallback: parse token as "provider:sub"
	if s.devMode {
		parts := strings.SplitN(token, ":", 2)
		if len(parts) == 2 {
			return parts[0], parts[1], nil
		}
	}

	// Real OIDC verification
	// ... (existing code unchanged)
```

**Step 5: Update cmd/relay/main.go**

Add `API_KEY_SECRET` loading and blocklist initialization before `relay.NewServer`:

```go
// API key signing secret
apiKeySecret := []byte(os.Getenv("API_KEY_SECRET"))
if len(apiKeySecret) == 0 {
	generated := make([]byte, 32)
	rand.Read(generated)
	apiKeySecret = generated
	logger.Warn("API_KEY_SECRET not set — generated random secret; API keys will not survive restarts")
}

// API key revocation blocklist
revocationFile := os.Getenv("API_KEY_REVOCATION_FILE")
if revocationFile == "" {
	revocationFile = "/etc/phosphor/revoked-keys.txt"
}
blocklist := relay.NewBlocklist(revocationFile)
defer blocklist.Stop()
```

Update the `NewServer` call to pass the new parameters.

**Step 6: Update .env-template**

Add to `.env-template`:

```
# API Keys
API_KEY_SECRET=
API_KEY_REVOCATION_FILE=
```

**Step 7: Fix all existing tests and call sites**

All existing tests that construct `Server` structs or call `NewServer` need the two new parameters. Search for all `NewServer(` calls and `&Server{` literals and add `apiKeySecret: nil, blocklist: nil` (or appropriate values).

**Step 8: Run all tests**

Run: `go test ./... -count=1`
Expected: PASS

**Step 9: Commit**

```
feat: wire API key auth into relay server and verifyToken
```

---

### Task 4: API Key Generation Endpoint (POST /api/auth/api-key)

**Files:**
- Modify: `internal/relay/handler_auth.go`
- Modify: `internal/relay/handler_auth_test.go`
- Modify: `internal/relay/server.go` (add route)

**Step 1: Write the tests**

Add to `internal/relay/handler_auth_test.go`:

```go
func TestHandleGenerateAPIKey_Success(t *testing.T) {
	s := newTestAuthServer(t)
	s.apiKeySecret = []byte("test-secret-32-bytes-long-xxxxx")

	r := httptest.NewRequest(http.MethodPost, "/api/auth/api-key", nil)
	r.Header.Set("Authorization", "Bearer dev:user@example.com")
	w := httptest.NewRecorder()

	s.HandleGenerateAPIKey(w, r)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var result struct {
		APIKey string `json:"api_key"`
		KeyID  string `json:"key_id"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	if result.APIKey == "" {
		t.Error("api_key is empty")
	}
	if !strings.HasPrefix(result.APIKey, "phk:") {
		t.Errorf("api_key %q does not have phk: prefix", result.APIKey)
	}
	if result.KeyID == "" {
		t.Error("key_id is empty")
	}
}

func TestHandleGenerateAPIKey_Unauthenticated(t *testing.T) {
	s := newTestAuthServer(t)
	s.devMode = false
	s.apiKeySecret = []byte("test-secret-32-bytes-long-xxxxx")

	r := httptest.NewRequest(http.MethodPost, "/api/auth/api-key", nil)
	w := httptest.NewRecorder()

	s.HandleGenerateAPIKey(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/relay/ -run TestHandleGenerateAPIKey -v -count=1`
Expected: FAIL — `HandleGenerateAPIKey` undefined

**Step 3: Write the handler**

Add to `internal/relay/handler_auth.go`:

```go
// HandleGenerateAPIKey generates a new API key for the authenticated user.
// POST /api/auth/api-key
func (s *Server) HandleGenerateAPIKey(w http.ResponseWriter, r *http.Request) {
	provider, sub, err := s.extractIdentity(r)
	if err != nil {
		http.Error(w, `{"error":"authentication required"}`, http.StatusUnauthorized)
		return
	}

	key, keyID, err := GenerateAPIKey(s.apiKeySecret, provider, sub)
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"api_key": "phk:" + key,
		"key_id":  keyID,
	})
}
```

**Step 4: Register the route in server.go**

Add to the `Handler()` method:

```go
mux.HandleFunc("POST /api/auth/api-key", s.HandleGenerateAPIKey)
```

**Step 5: Run tests**

Run: `go test ./internal/relay/ -run TestHandleGenerateAPIKey -v -count=1`
Expected: PASS

**Step 6: Commit**

```
feat: add POST /api/auth/api-key endpoint
```

---

### Task 5: Daemon Auth Failure Retry Limit

**Files:**
- Modify: `internal/daemon/daemon.go`
- Modify: `internal/daemon/daemon_test.go`

**Step 1: Write the test**

Add to `internal/daemon/daemon_test.go`:

```go
func TestIsAuthError(t *testing.T) {
	tests := []struct {
		err  error
		want bool
	}{
		{fmt.Errorf("server error: auth_failed: bad token"), true},
		{fmt.Errorf("server error: invalid_token: expired"), true},
		{fmt.Errorf("dial ws://localhost:8080/ws/cli: connection refused"), false},
		{fmt.Errorf("receive welcome: EOF"), false},
		{nil, false},
	}
	for _, tt := range tests {
		got := isAuthError(tt.err)
		if got != tt.want {
			t.Errorf("isAuthError(%v) = %v, want %v", tt.err, got, tt.want)
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/daemon/ -run TestIsAuthError -v -count=1`
Expected: FAIL — `isAuthError` undefined

**Step 3: Implement auth error detection and retry limit**

Add to `internal/daemon/daemon.go`:

```go
// isAuthError returns true if the error indicates an authentication failure
// (as opposed to a transient network error).
func isAuthError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "server error: auth_failed") ||
		strings.Contains(msg, "server error: invalid_token")
}
```

Update `runMapping` to track auth failures and stop after 3:

```go
func (d *Daemon) runMapping(ctx context.Context, mapping Mapping) {
	attempt := 0
	authFailures := 0
	const maxAuthFailures = 3

	for {
		if ctx.Err() != nil {
			return
		}

		err := d.runConnection(ctx, mapping)
		if ctx.Err() != nil {
			return
		}

		if err != nil {
			if isAuthError(err) {
				authFailures++
				d.Logger.Error("authentication failed",
					"identity", mapping.Identity,
					"attempt", authFailures,
					"max", maxAuthFailures,
					"err", err,
				)
				if authFailures >= maxAuthFailures {
					d.Logger.Error("max auth failures reached, stopping mapping",
						"identity", mapping.Identity,
					)
					return
				}
			} else {
				authFailures = 0 // reset on non-auth errors
			}

			d.Logger.Warn("connection failed",
				"identity", mapping.Identity,
				"attempt", attempt,
				"err", err,
			)
		}

		delay := backoffSchedule[min(attempt, len(backoffSchedule)-1)]
		attempt++

		d.Logger.Info("reconnecting",
			"identity", mapping.Identity,
			"delay", delay,
			"attempt", attempt,
		)

		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return
		}
	}
}
```

Add `"strings"` to the import block in `daemon.go`.

**Step 4: Run tests**

Run: `go test ./internal/daemon/ -run TestIsAuthError -v -count=1`
Expected: PASS

**Step 5: Commit**

```
feat: stop daemon mapping after 3 auth failures
```

---

### Task 6: Daemon Config & CLI (api_key field + set-key command)

**Files:**
- Modify: `internal/daemon/config.go`
- Modify: `internal/daemon/config_test.go`
- Modify: `cmd/phosphor/daemon.go`

**Step 1: Write the test**

Add to `internal/daemon/config_test.go`:

```go
func TestConfig_ApiKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.json")

	cfg := &Config{
		Relay:  "wss://example.com",
		ApiKey: "phk:test-key",
	}
	if err := WriteConfig(path, cfg); err != nil {
		t.Fatalf("WriteConfig: %v", err)
	}

	loaded, err := ReadConfig(path)
	if err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}
	if loaded.ApiKey != "phk:test-key" {
		t.Errorf("ApiKey = %q, want phk:test-key", loaded.ApiKey)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/daemon/ -run TestConfig_ApiKey -v -count=1`
Expected: FAIL — `ApiKey` field unknown

**Step 3: Add ApiKey to Config**

In `internal/daemon/config.go`:

```go
type Config struct {
	Relay    string    `json:"relay"`
	ApiKey   string    `json:"api_key,omitempty"`
	Mappings []Mapping `json:"mappings"`
}
```

**Step 4: Update daemon run command**

In `cmd/phosphor/daemon.go`, replace the `LoadTokenCache` block with:

```go
if cfg.ApiKey == "" {
	return fmt.Errorf("no api_key in config — generate one in the web UI and run 'phosphor daemon set-key <key>'")
}

d := &daemon.Daemon{
	Config:     cfg,
	Token:      cfg.ApiKey,
	Logger:     logger,
	ConfigPath: configPath,
	Spawn:      daemon.StartPTYAsUser,
}
```

**Step 5: Add set-key subcommand**

In `cmd/phosphor/daemon.go`, add inside `newDaemonCmd()`:

```go
setKeyCmd := &cobra.Command{
	Use:   "set-key [key]",
	Short: "Set the API key for daemon authentication",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if configPath == "" {
			configPath = daemon.DefaultConfigPath()
		}
		cfg, err := daemon.ReadConfig(configPath)
		if err != nil {
			return fmt.Errorf("no config at %s — run 'phosphor daemon map' first: %w", configPath, err)
		}
		cfg.ApiKey = args[0]
		if err := daemon.WriteConfig(configPath, cfg); err != nil {
			return err
		}
		fmt.Println("API key set successfully.")
		return nil
	},
}
```

Add `setKeyCmd` to `daemonCmd.AddCommand(...)`.

**Step 6: Run tests**

Run: `go test ./internal/daemon/ -count=1`
Expected: PASS

**Step 7: Commit**

```
feat: add api_key to daemon config and set-key CLI command
```

---

### Task 7: SPA — Settings Page & API Key Generation

**Files:**
- Create: `web/src/components/SettingsPage.tsx`
- Modify: `web/src/lib/api.ts`
- Modify: `web/src/App.tsx`
- Modify: `web/src/components/Layout.tsx`

**Step 1: Add generateApiKey to api.ts**

```typescript
export interface ApiKeyResponse {
  api_key: string;
  key_id: string;
}

export async function generateApiKey(token: string | null): Promise<ApiKeyResponse> {
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
  };
  if (token) {
    headers["Authorization"] = `Bearer ${token}`;
  }

  const res = await fetch(`${BASE}/api/auth/api-key`, {
    method: "POST",
    headers,
  });
  if (!res.ok) {
    throw new Error(`Failed to generate API key: ${res.status}`);
  }
  return res.json() as Promise<ApiKeyResponse>;
}
```

**Step 2: Create SettingsPage.tsx**

```tsx
import { useState } from "react";
import { useAuth } from "../auth/useAuth";
import { generateApiKey } from "../lib/api";

export function SettingsPage() {
  const { getToken } = useAuth();
  const [apiKey, setApiKey] = useState<string | null>(null);
  const [keyId, setKeyId] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [generating, setGenerating] = useState(false);

  const handleGenerate = async () => {
    setError(null);
    setGenerating(true);
    try {
      const result = await generateApiKey(getToken());
      setApiKey(result.api_key);
      setKeyId(result.key_id);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to generate API key");
    } finally {
      setGenerating(false);
    }
  };

  const handleCopy = () => {
    if (apiKey) {
      navigator.clipboard.writeText(apiKey);
    }
  };

  return (
    <div style={{ maxWidth: 640 }}>
      <h2 style={{ color: "var(--green)", marginBottom: 24 }}>Settings</h2>

      <section>
        <h3 style={{ color: "var(--text)", marginBottom: 12 }}>API Keys</h3>
        <p style={{ color: "var(--text)", fontSize: 13, marginBottom: 16 }}>
          Generate an API key to authenticate the phosphor daemon. The key is
          shown once — copy it and store it securely.
        </p>

        {apiKey ? (
          <div>
            <div
              style={{
                background: "var(--bg-card)",
                border: "1px solid var(--border)",
                padding: 12,
                marginBottom: 8,
                wordBreak: "break-all",
                fontSize: 12,
                color: "var(--green)",
              }}
            >
              {apiKey}
            </div>
            <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
              <button className="btn-primary" onClick={handleCopy}>
                copy to clipboard
              </button>
              <span style={{ color: "var(--text)", fontSize: 12 }}>
                Key ID: {keyId}
              </span>
            </div>
            <p
              style={{
                color: "var(--amber)",
                fontSize: 12,
                marginTop: 12,
              }}
            >
              This key will not be shown again. Install it on your daemon with:
            </p>
            <pre
              style={{
                color: "var(--text)",
                fontSize: 12,
                background: "var(--bg-card)",
                border: "1px solid var(--border)",
                padding: 8,
                marginTop: 4,
              }}
            >
              sudo phosphor daemon set-key {apiKey}
            </pre>
          </div>
        ) : (
          <div>
            <button
              className="btn-primary"
              onClick={() => void handleGenerate()}
              disabled={generating}
            >
              {generating ? "generating..." : "generate API key"}
            </button>
            {error && (
              <p style={{ color: "var(--red)", fontSize: 12, marginTop: 8 }}>
                {error}
              </p>
            )}
          </div>
        )}
      </section>
    </div>
  );
}
```

**Step 3: Add route in App.tsx**

Import `SettingsPage` and add the route inside the `<Route element={<Layout />}>` block:

```tsx
<Route
  path="/settings"
  element={
    <ProtectedRoute>
      <SettingsPage />
    </ProtectedRoute>
  }
/>
```

**Step 4: Add settings link in Layout.tsx**

Add a link to `/settings` in the header, next to the user email (when logged in):

```tsx
<Link
  to="/settings"
  style={{ color: "var(--text)", fontSize: 12, textDecoration: "none" }}
>
  settings
</Link>
```

**Step 5: Build and verify**

Run: `cd web && npm run build`
Expected: Build succeeds

**Step 6: Commit**

```
feat: add settings page with API key generation
```

---

### Task 8: Update .env-template and Final Integration

**Files:**
- Modify: `.env-template`

**Step 1: Add API key env vars**

```
# API Keys
# Secret used to sign daemon API keys (HS256). If not set, a random secret is
# generated on startup (keys will not survive relay restarts).
API_KEY_SECRET=
# Path to file containing revoked API key IDs, one per line.
API_KEY_REVOCATION_FILE=
```

**Step 2: Run full test suite**

Run: `go test ./... -count=1`
Run: `cd web && npx vitest run`
Run: `cd web && npm run build`
Expected: All pass

**Step 3: Commit**

```
feat: add API key configuration to .env-template
```

---
