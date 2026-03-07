package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// Mapping maps a web identity to a local user account and shell.
type Mapping struct {
	Identity  string `json:"identity"`
	LocalUser string `json:"local_user"`
	Shell     string `json:"shell"`
}

// Config holds daemon configuration.
type Config struct {
	Relay    string    `json:"relay"`
	ApiKey   string    `json:"api_key,omitempty"`
	Mappings []Mapping `json:"mappings"`
}

// DefaultConfigPath returns the platform-specific config file path.
func DefaultConfigPath() string {
	if runtime.GOOS == "windows" {
		return `C:\ProgramData\phosphor\daemon.json`
	}
	return "/etc/phosphor/daemon.json"
}

// ReadConfig reads the daemon config from disk.
func ReadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &cfg, nil
}

// WriteConfig writes the daemon config to disk.
func WriteConfig(path string, cfg *Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// AddMapping adds or updates a mapping by identity.
func (c *Config) AddMapping(m Mapping) {
	for i, existing := range c.Mappings {
		if existing.Identity == m.Identity {
			c.Mappings[i] = m
			return
		}
	}
	c.Mappings = append(c.Mappings, m)
}

// RemoveMapping removes a mapping by identity. Returns true if found.
func (c *Config) RemoveMapping(identity string) bool {
	for i, m := range c.Mappings {
		if m.Identity == identity {
			c.Mappings = append(c.Mappings[:i], c.Mappings[i+1:]...)
			return true
		}
	}
	return false
}
