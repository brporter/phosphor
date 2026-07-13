package cli

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"
)

// MachineConfig identifies this machine's enrollment with a relay.
type MachineConfig struct {
	MachineID string `json:"machine_id"`
	RelayURL  string `json:"relay_url"`
	// SSHAddr is the gateway's host:port, learned at enrollment.
	SSHAddr string `json:"ssh_addr"`
	// HostKey is the gateway's public host key in authorized_keys format,
	// pinned at enrollment over TLS.
	HostKey string `json:"host_key"`
	// SSHDAddr is the local sshd the tunnel exposes.
	SSHDAddr string `json:"sshd_addr,omitempty"`
}

const (
	machineKeyFile    = "machine_key"
	machineConfigFile = "machine.json"
)

// GenerateMachineKey creates an ed25519 machine keypair, persists the
// private key, and returns the signer.
func GenerateMachineKey() (ssh.Signer, error) {
	dir, err := configDir()
	if err != nil {
		return nil, err
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generating machine key: %w", err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "phosphor machine key")
	if err != nil {
		return nil, fmt.Errorf("marshaling machine key: %w", err)
	}
	path := filepath.Join(dir, machineKeyFile)
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0600); err != nil {
		return nil, fmt.Errorf("writing machine key: %w", err)
	}
	return ssh.NewSignerFromKey(priv)
}

// LoadMachineKey reads the persisted machine key.
func LoadMachineKey() (ssh.Signer, error) {
	dir, err := configDir()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, machineKeyFile))
	if err != nil {
		return nil, err
	}
	return ssh.ParsePrivateKey(data)
}

// SaveMachineConfig persists the machine's enrollment record.
func SaveMachineConfig(cfg *MachineConfig) error {
	dir, err := configDir()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, machineConfigFile), data, 0600)
}

// LoadMachineConfig reads the machine's enrollment record.
func LoadMachineConfig() (*MachineConfig, error) {
	dir, err := configDir()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, machineConfigFile))
	if err != nil {
		return nil, err
	}
	var cfg MachineConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
