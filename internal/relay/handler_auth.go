package relay

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/brporter/phosphor/internal/auth"
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

// --- Request/Response types ---

type authLoginRequest struct {
	Provider string `json:"provider"`
	Source   string `json:"source"` // "web", "mobile", or "cli" (default)
}

type authLoginResponse struct {
	SessionID string `json:"session_id"`
	AuthURL   string `json:"auth_url"`
}

// HandleAuthConfig returns the list of available authentication providers.
// GET /api/auth/config
func (s *Server) HandleAuthConfig(w http.ResponseWriter, r *http.Request) {
	providers := s.verifier.ProviderNames()
	if s.devMode {
		providers = append([]string{"dev"}, providers...)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"providers": providers})
}

// generateDevToken creates a synthetic unsigned JWT for dev-mode authentication.
func generateDevToken() string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"typ":"JWT","alg":"none"}`))
	exp := time.Now().Add(24 * time.Hour).Unix()
	payload := fmt.Sprintf(`{"sub":"dev-user","iss":"dev","email":"dev@localhost","exp":%d}`, exp)
	payloadB64 := base64.RawURLEncoding.EncodeToString([]byte(payload))
	return header + "." + payloadB64 + "."
}

// HandleAuthLogin starts a relay-mediated browser auth flow.
// POST /api/auth/login  body: {"provider":"apple"|"microsoft"|"google"|"dev"}
func (s *Server) HandleAuthLogin(w http.ResponseWriter, r *http.Request) {
	var req authLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	// Dev provider: short-circuit the OIDC flow.
	if req.Provider == "dev" {
		if !s.devMode {
			http.Error(w, `{"error":"unknown provider"}`, http.StatusBadRequest)
			return
		}

		source := req.Source
		if source == "" {
			source = "cli"
		}

		sess, err := s.authSessions.Create(r.Context(), "dev", "", source)
		if err != nil {
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}

		s.authSessions.Complete(r.Context(), sess.ID, generateDevToken())

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(authLoginResponse{
			SessionID: sess.ID,
			AuthURL:   "",
		})
		return
	}

	if _, ok := s.verifier.GetProvider(req.Provider); !ok {
		http.Error(w, `{"error":"unknown provider"}`, http.StatusBadRequest)
		return
	}

	source := req.Source
	if source == "" {
		source = "cli"
	}

	verifier := generateCodeVerifier()
	sess, err := s.authSessions.Create(r.Context(), req.Provider, verifier, source)
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

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
	sess, ok, err := s.authSessions.Get(r.Context(), sessionID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
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
		errMsg := r.URL.Query().Get("error_description")
		if errMsg == "" {
			errMsg = r.URL.Query().Get("error")
		}
		if errMsg == "" {
			errMsg = r.FormValue("error_description")
		}
		if errMsg == "" {
			errMsg = r.FormValue("error")
		}
		if errMsg == "" {
			errMsg = "missing code or state"
		}
		s.renderAuthResult(w, false, errMsg)
		return
	}

	ctx := r.Context()
	sess, ok, err := s.authSessions.Get(ctx, state)
	if err != nil {
		s.renderAuthResult(w, false, "internal error")
		return
	}
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

	redirectURI := fmt.Sprintf("%s/api/auth/callback", s.baseURL)
	data := url.Values{
		"client_id":     {cfg.ClientID},
		"code":          {code},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {redirectURI},
		"code_verifier": {sess.CodeVerifier},
	}

	// Add client_secret -- for Apple, generate it dynamically
	if sess.Provider == "apple" && cfg.PrivateKey != nil {
		secret, err := auth.GenerateAppleClientSecret(cfg.TeamID, cfg.ClientID, cfg.KeyID, cfg.PrivateKey)
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

	s.authSessions.Complete(ctx, state, tokenResult.IDToken)

	// Web-originated logins redirect back to the SPA; mobile logins redirect
	// to the phosphor:// custom scheme; CLI logins show a success page.
	switch sess.Source {
	case "web":
		http.Redirect(w, r, s.baseURL, http.StatusFound)
	case "mobile":
		target := fmt.Sprintf("phosphor://auth/callback?session=%s", url.QueryEscape(state))
		http.Redirect(w, r, target, http.StatusFound)
	default:
		s.renderAuthResult(w, true, "")
	}
}

// HandleAuthPoll checks if a login session has completed.
// GET /api/auth/poll?session=SESSION_ID
func (s *Server) HandleAuthPoll(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session")

	token, ok, err := s.authSessions.Consume(r.Context(), sessionID)
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if ok {
		json.NewEncoder(w).Encode(map[string]string{"status": "complete", "id_token": token})
	} else {
		json.NewEncoder(w).Encode(map[string]string{"status": "pending"})
	}
}

// HandleCLIStart creates an auth session with no provider selected yet.
// POST /api/auth/cli-start
func (s *Server) HandleCLIStart(w http.ResponseWriter, r *http.Request) {
	sess, err := s.authSessions.Create(r.Context(), "", "", "cli")
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"session_id": sess.ID})
}

// HandleCLILogin serves a minimal HTML provider-picker page.
// GET /api/auth/cli-login?session=SESSION_ID
func (s *Server) HandleCLILogin(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session")
	if sessionID == "" {
		http.Error(w, "missing session parameter", http.StatusBadRequest)
		return
	}

	_, ok, err := s.authSessions.Get(r.Context(), sessionID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "invalid or expired session", http.StatusBadRequest)
		return
	}

	providers := s.verifier.ProviderNames()
	var buttons string
	for _, p := range providers {
		label := strings.ToUpper(p[:1]) + p[1:]
		buttons += fmt.Sprintf(
			`<button type="submit" name="provider" value="%s" style="display:block;width:100%%;padding:12px 24px;margin:8px 0;background:#1a1a1a;color:#00ff41;border:1px solid #00ff41;font-family:monospace;font-size:16px;cursor:pointer">Sign in with %s</button>`,
			html.EscapeString(p), html.EscapeString(label),
		)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head><title>Phosphor — Sign In</title></head>
<body style="background:#0a0a0a;color:#00ff41;font-family:monospace;display:flex;justify-content:center;align-items:center;height:100vh;margin:0">
<div style="text-align:center;max-width:320px">
<h2>Phosphor</h2>
<p>Choose a sign-in provider:</p>
<form method="POST" action="%s/api/auth/cli-choose">
<input type="hidden" name="session" value="%s">
%s
</form>
</div>
</body>
</html>`,
		html.EscapeString(s.baseURL),
		html.EscapeString(sessionID),
		buttons,
	)
}

// HandleCLIChoose receives the provider choice, sets up PKCE, and redirects to OIDC.
// POST /api/auth/cli-choose  form: session=ID&provider=microsoft
func (s *Server) HandleCLIChoose(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form data", http.StatusBadRequest)
		return
	}

	sessionID := r.FormValue("session")
	provider := r.FormValue("provider")

	if _, ok := s.verifier.GetProvider(provider); !ok {
		http.Error(w, "unknown provider", http.StatusBadRequest)
		return
	}

	_, ok, err := s.authSessions.Get(r.Context(), sessionID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "invalid or expired session", http.StatusBadRequest)
		return
	}

	verifier := generateCodeVerifier()
	if err := s.authSessions.SetProvider(r.Context(), sessionID, provider, verifier); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Redirect to the authorize endpoint which reads provider+verifier from the session
	target := fmt.Sprintf("%s/api/auth/authorize?session=%s", s.baseURL, url.QueryEscape(sessionID))
	http.Redirect(w, r, target, http.StatusFound)
}

// HandleGenerateAPIKey generates a new API key for the authenticated user.
// POST /api/auth/api-key
func (s *Server) HandleGenerateAPIKey(w http.ResponseWriter, r *http.Request) {
	provider, sub, email, err := s.extractIdentity(r)
	if err != nil {
		http.Error(w, `{"error":"authentication required"}`, http.StatusUnauthorized)
		return
	}

	// Prefer email over opaque sub for the API key identity, since daemon
	// mappings typically reference users by email address.
	identity := sub
	if email != "" {
		identity = email
	}
	key, keyID, err := GenerateAPIKey(s.apiKeySecret, provider, identity)
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

func (s *Server) renderAuthResult(w http.ResponseWriter, success bool, errMsg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if success {
		fmt.Fprintf(w, `<!DOCTYPE html><html><body style="background:#0a0a0a;color:#00ff41;font-family:monospace;display:flex;justify-content:center;align-items:center;height:100vh;margin:0"><div style="text-align:center"><h2>Authentication Complete</h2><p>You can close this tab and return to your terminal.</p><p style="margin-top:1em"><a href="%s" style="color:#00ff41">View your sessions</a></p></div></body></html>`, html.EscapeString(s.baseURL))
	} else {
		safeMsg := html.EscapeString(errMsg)
		fmt.Fprintf(w, `<!DOCTYPE html><html><body style="background:#0a0a0a;color:#ff4444;font-family:monospace;display:flex;justify-content:center;align-items:center;height:100vh;margin:0"><div style="text-align:center"><h2>Authentication Failed</h2><p>%s</p></div></body></html>`, safeMsg)
	}
}
