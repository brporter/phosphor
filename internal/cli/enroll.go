package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// EnrollOptions configures machine enrollment.
type EnrollOptions struct {
	RelayURL string
	Name     string // defaults to hostname
	APIKey   string // "phk:..." for headless enrollment; browser login if empty
	SSHDAddr string // local sshd the tunnel will expose (default 127.0.0.1:22)
}

type sshInfoResponse struct {
	Addr        string `json:"addr"`
	HostKey     string `json:"host_key"`
	Fingerprint string `json:"fingerprint"`
}

type createMachineRequest struct {
	Name      string `json:"name"`
	Hostname  string `json:"hostname"`
	PublicKey string `json:"public_key"`
}

type createMachineResponse struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Error string `json:"error"`
}

// Enroll registers this machine with the relay: it authenticates the user,
// generates a machine keypair, registers the public key, and pins the
// gateway's SSH endpoint and host key (fetched over TLS, so there is no
// trust-on-first-use hole).
func Enroll(ctx context.Context, opts EnrollOptions) (*MachineConfig, error) {
	baseURL := httpBaseURL(opts.RelayURL)

	token := opts.APIKey
	if token == "" {
		if cache, err := LoadTokenCache(); err == nil && cache.AccessToken != "" {
			token = cache.AccessToken
		}
	}
	if token == "" {
		var err error
		token, err = BrowserLogin(ctx, opts.RelayURL)
		if err != nil {
			return nil, fmt.Errorf("authenticating: %w", err)
		}
	}

	name := opts.Name
	hostname, _ := os.Hostname()
	if name == "" {
		name = hostname
	}
	if name == "" {
		return nil, fmt.Errorf("--name is required when the hostname cannot be determined")
	}

	signer, err := GenerateMachineKey()
	if err != nil {
		return nil, err
	}
	pubKey := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))

	client := &http.Client{Timeout: 30 * time.Second}

	// Register the machine.
	body, _ := json.Marshal(createMachineRequest{Name: name, Hostname: hostname, PublicKey: pubKey})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/machines", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("registering machine: %w", err)
	}
	defer resp.Body.Close()
	var created createMachineResponse
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return nil, fmt.Errorf("decoding enrollment response (status %d): %w", resp.StatusCode, err)
	}
	if resp.StatusCode != http.StatusCreated {
		if created.Error != "" {
			return nil, fmt.Errorf("enrollment failed: %s", created.Error)
		}
		return nil, fmt.Errorf("enrollment failed with status %d", resp.StatusCode)
	}

	// Pin the SSH gateway endpoint + host key.
	req, err = http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/ssh-info", nil)
	if err != nil {
		return nil, err
	}
	infoResp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching ssh gateway info: %w", err)
	}
	defer infoResp.Body.Close()
	var info sshInfoResponse
	if err := json.NewDecoder(infoResp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("decoding ssh gateway info: %w", err)
	}
	if info.Addr == "" || info.HostKey == "" {
		return nil, fmt.Errorf("relay did not provide ssh gateway info")
	}

	cfg := &MachineConfig{
		MachineID: created.ID,
		RelayURL:  opts.RelayURL,
		SSHAddr:   info.Addr,
		HostKey:   info.HostKey,
		SSHDAddr:  opts.SSHDAddr,
	}
	if err := SaveMachineConfig(cfg); err != nil {
		return nil, fmt.Errorf("saving machine config: %w", err)
	}
	return cfg, nil
}

// httpBaseURL normalizes a relay URL (possibly ws:// or wss://) to http(s).
func httpBaseURL(relayURL string) string {
	u := strings.TrimSuffix(relayURL, "/")
	u = strings.Replace(u, "ws://", "http://", 1)
	u = strings.Replace(u, "wss://", "https://", 1)
	return u
}
