package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/brporter/phosphor/internal/auth"
)

// Provider configs for device code flow.
var providerConfigs = map[string]struct {
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

// Login performs device code authentication for the given provider.
func Login(ctx context.Context, providerName string) error {
	p, ok := providerConfigs[providerName]
	if !ok {
		return fmt.Errorf("unknown provider: %s (supported: microsoft, google)", providerName)
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
		Provider:     strings.ToLower(providerName),
	}); err != nil {
		return fmt.Errorf("save token: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Authenticated successfully!\n")
	return nil
}
