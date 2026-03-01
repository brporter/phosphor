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

var supportedProviders = []string{"apple", "microsoft", "google"}

// Login performs authentication for the given provider.
func Login(ctx context.Context, providerName, relayURL string, useDeviceCode bool) error {
	providerName = strings.ToLower(providerName)

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

	if useDeviceCode {
		return loginDeviceCode(ctx, providerName)
	}

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
