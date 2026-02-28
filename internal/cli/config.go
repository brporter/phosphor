package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Config holds CLI configuration.
type Config struct {
	RelayURL string `json:"relay_url"`
}

// DefaultConfig returns the default configuration.
func DefaultConfig() Config {
	return Config{
		RelayURL: "ws://localhost:8080",
	}
}

// TokenCache stores cached auth tokens.
type TokenCache struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	Provider     string `json:"provider"`
}

func configDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".config", "phosphor")
	return dir, os.MkdirAll(dir, 0700)
}

// LoadTokenCache reads the cached tokens from disk.
func LoadTokenCache() (*TokenCache, error) {
	dir, err := configDir()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, "tokens.json"))
	if err != nil {
		return nil, err
	}
	var cache TokenCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, err
	}
	return &cache, nil
}

// SaveTokenCache writes tokens to disk with restricted permissions.
func SaveTokenCache(cache *TokenCache) error {
	dir, err := configDir()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "tokens.json"), data, 0600)
}

// ClearTokenCache removes the cached tokens.
func ClearTokenCache() error {
	dir, err := configDir()
	if err != nil {
		return err
	}
	err = os.Remove(filepath.Join(dir, "tokens.json"))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
