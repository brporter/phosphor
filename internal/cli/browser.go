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

var openBrowserFn = openBrowser

// BrowserLogin performs relay-mediated browser-based authentication.
func BrowserLogin(ctx context.Context, relayURL, provider string) (string, error) {
	httpBase := relayURL
	httpBase = strings.Replace(httpBase, "ws://", "http://", 1)
	httpBase = strings.Replace(httpBase, "wss://", "https://", 1)

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

	fmt.Fprintf(os.Stderr, "\nOpening browser for authentication...\n")
	fmt.Fprintf(os.Stderr, "If the browser doesn't open, visit: %s\n\n", loginResp.AuthURL)
	openBrowserFn(loginResp.AuthURL)

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
