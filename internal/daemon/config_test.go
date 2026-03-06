package daemon

import (
	"path/filepath"
	"testing"
)

func TestConfigReadWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.json")

	cfg := &Config{
		Relay: "wss://example.com",
		Mappings: []Mapping{
			{Identity: "alice@example.com", LocalUser: "alice", Shell: "/bin/bash"},
		},
	}

	if err := WriteConfig(path, cfg); err != nil {
		t.Fatal(err)
	}

	loaded, err := ReadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Relay != "wss://example.com" {
		t.Errorf("Relay = %q", loaded.Relay)
	}
	if len(loaded.Mappings) != 1 || loaded.Mappings[0].Identity != "alice@example.com" {
		t.Errorf("Mappings = %+v", loaded.Mappings)
	}
}

func TestAddRemoveMapping(t *testing.T) {
	cfg := &Config{Relay: "wss://example.com"}

	cfg.AddMapping(Mapping{Identity: "a@b.com", LocalUser: "a", Shell: "/bin/bash"})
	if len(cfg.Mappings) != 1 {
		t.Fatal("expected 1 mapping")
	}

	// Update existing
	cfg.AddMapping(Mapping{Identity: "a@b.com", LocalUser: "a", Shell: "/bin/zsh"})
	if len(cfg.Mappings) != 1 || cfg.Mappings[0].Shell != "/bin/zsh" {
		t.Fatal("expected update, not append")
	}

	cfg.RemoveMapping("a@b.com")
	if len(cfg.Mappings) != 0 {
		t.Fatal("expected 0 mappings after remove")
	}

	if cfg.RemoveMapping("nonexistent") {
		t.Fatal("expected false for nonexistent removal")
	}
}

func TestReadConfig_NotFound(t *testing.T) {
	_, err := ReadConfig("/nonexistent/path/daemon.json")
	if err == nil {
		t.Fatal("expected error for missing config")
	}
}
