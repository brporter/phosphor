package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DeviceCodeResponse is the response from the device authorization endpoint.
type DeviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// DeviceTokenResponse is the response from the token endpoint.
type DeviceTokenResponse struct {
	AccessToken  string `json:"access_token"`
	IDToken      string `json:"id_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

// RequestDeviceCode initiates the device code flow.
func RequestDeviceCode(ctx context.Context, deviceAuthURL, clientID string, scopes []string) (*DeviceCodeResponse, error) {
	data := url.Values{
		"client_id": {clientID},
		"scope":     {strings.Join(scopes, " ")},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", deviceAuthURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("device auth request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("device auth failed (%d): %s", resp.StatusCode, body)
	}

	var dcr DeviceCodeResponse
	if err := json.Unmarshal(body, &dcr); err != nil {
		return nil, fmt.Errorf("decode device code response: %w", err)
	}
	if dcr.Interval == 0 {
		dcr.Interval = 5
	}
	return &dcr, nil
}

// PollForToken polls the token endpoint until the user completes authentication.
func PollForToken(ctx context.Context, tokenURL, clientID, deviceCode string) (*DeviceTokenResponse, error) {
	data := url.Values{
		"client_id":   {clientID},
		"device_code": {deviceCode},
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
	}

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		req, err := http.NewRequestWithContext(ctx, "POST", tokenURL, strings.NewReader(data.Encode()))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("token request: %w", err)
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		var dtr DeviceTokenResponse
		if err := json.Unmarshal(body, &dtr); err != nil {
			return nil, fmt.Errorf("decode token response: %w", err)
		}

		switch dtr.Error {
		case "":
			return &dtr, nil
		case "authorization_pending":
			time.Sleep(5 * time.Second)
			continue
		case "slow_down":
			time.Sleep(10 * time.Second)
			continue
		case "expired_token":
			return nil, fmt.Errorf("device code expired — please try again")
		case "access_denied":
			return nil, fmt.Errorf("access denied by user")
		default:
			return nil, fmt.Errorf("token error: %s: %s", dtr.Error, dtr.ErrorDesc)
		}
	}
}
