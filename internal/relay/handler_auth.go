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
		json.NewEncoder(w).Encode(map[string]string{"status": "pending"})
	}
}

func (s *Server) renderAuthResult(w http.ResponseWriter, success bool, errMsg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if success {
		fmt.Fprint(w, `<!DOCTYPE html><html><body style="background:#0a0a0a;color:#00ff41;font-family:monospace;display:flex;justify-content:center;align-items:center;height:100vh;margin:0"><div style="text-align:center"><h2>Authentication Complete</h2><p>You can close this tab and return to your terminal.</p></div></body></html>`)
	} else {
		safeMsg := html.EscapeString(errMsg)
		fmt.Fprintf(w, `<!DOCTYPE html><html><body style="background:#0a0a0a;color:#ff4444;font-family:monospace;display:flex;justify-content:center;align-items:center;height:100vh;margin:0"><div style="text-align:center"><h2>Authentication Failed</h2><p>%s</p></div></body></html>`, safeMsg)
	}
}
