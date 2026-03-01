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

type cliStartResponse struct {
	SessionID string `json:"session_id"`
}

type pollResponse struct {
	Status  string `json:"status"`
	IDToken string `json:"id_token"`
}

var openBrowserFn = openBrowser

// BrowserLogin performs relay-mediated browser-based authentication.
// The user picks their provider in the browser via the relay's provider-picker page.
func BrowserLogin(ctx context.Context, relayURL string) (string, error) {
	httpBase := relayURL
	httpBase = strings.Replace(httpBase, "ws://", "http://", 1)
	httpBase = strings.Replace(httpBase, "wss://", "https://", 1)

	resp, err := http.Post(httpBase+"/api/auth/cli-start", "application/json", strings.NewReader("{}"))
	if err != nil {
		return "", fmt.Errorf("start auth session: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("relay returned %d starting auth", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		return "", fmt.Errorf("relay does not support auto-login (got %s) — upgrade relay or use 'phosphor login'", ct)
	}

	var startResp cliStartResponse
	if err := json.NewDecoder(resp.Body).Decode(&startResp); err != nil {
		return "", fmt.Errorf("decode cli-start response: %w", err)
	}

	loginURL := fmt.Sprintf("%s/api/auth/cli-login?session=%s", httpBase, startResp.SessionID)

	fmt.Fprintf(os.Stderr, "\nOpening browser for authentication...\n")
	fmt.Fprintf(os.Stderr, "If the browser doesn't open, visit: %s\n\n", loginURL)
	openBrowserFn(loginURL)

	fmt.Fprintf(os.Stderr, "Waiting for authentication...\n")
	pollURL := fmt.Sprintf("%s/api/auth/poll?session=%s", httpBase, startResp.SessionID)

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
