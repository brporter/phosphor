package cli

import (
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.RelayURL != DefaultRelayURL {
		t.Errorf("expected RelayURL %q, got %q", DefaultRelayURL, cfg.RelayURL)
	}
}

func TestSaveAndLoadTokenCache(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("USERPROFILE", tmpDir)
	t.Setenv("HOME", tmpDir)

	cache := &TokenCache{
		AccessToken:  "access-token-value",
		RefreshToken: "refresh-token-value",
		Provider:     "microsoft",
	}

	if err := SaveTokenCache(cache); err != nil {
		t.Fatalf("SaveTokenCache failed: %v", err)
	}

	loaded, err := LoadTokenCache()
	if err != nil {
		t.Fatalf("LoadTokenCache failed: %v", err)
	}

	if loaded.AccessToken != cache.AccessToken {
		t.Errorf("AccessToken: got %q, want %q", loaded.AccessToken, cache.AccessToken)
	}
	if loaded.RefreshToken != cache.RefreshToken {
		t.Errorf("RefreshToken: got %q, want %q", loaded.RefreshToken, cache.RefreshToken)
	}
	if loaded.Provider != cache.Provider {
		t.Errorf("Provider: got %q, want %q", loaded.Provider, cache.Provider)
	}
}

func TestLoadTokenCache_NotExists(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("USERPROFILE", tmpDir)
	t.Setenv("HOME", tmpDir)

	_, err := LoadTokenCache()
	if err == nil {
		t.Error("expected error when tokens.json does not exist, got nil")
	}
}

func TestClearTokenCache(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("USERPROFILE", tmpDir)
	t.Setenv("HOME", tmpDir)

	cache := &TokenCache{
		AccessToken: "token",
		Provider:    "google",
	}

	if err := SaveTokenCache(cache); err != nil {
		t.Fatalf("SaveTokenCache failed: %v", err)
	}

	if err := ClearTokenCache(); err != nil {
		t.Fatalf("ClearTokenCache failed: %v", err)
	}

	_, err := LoadTokenCache()
	if err == nil {
		t.Error("expected error after clearing token cache, got nil")
	}
}

func TestClearTokenCache_NotExists(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("USERPROFILE", tmpDir)
	t.Setenv("HOME", tmpDir)

	if err := ClearTokenCache(); err != nil {
		t.Errorf("ClearTokenCache on missing file should not error, got: %v", err)
	}
}
