//go:build linux

package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const serviceUnitPath = "/etc/systemd/system/phosphor-daemon.service"

// Install creates and enables the systemd service.
func Install(binaryPath string) error {
	if binaryPath == "" {
		var err error
		binaryPath, err = os.Executable()
		if err != nil {
			return fmt.Errorf("resolve executable: %w", err)
		}
		binaryPath, _ = filepath.Abs(binaryPath)
	}

	unit := fmt.Sprintf(`[Unit]
Description=Phosphor Terminal Sharing Daemon
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s daemon run
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
`, binaryPath)

	if err := os.WriteFile(serviceUnitPath, []byte(unit), 0644); err != nil {
		return fmt.Errorf("write unit file: %w", err)
	}

	if err := exec.Command("systemctl", "daemon-reload").Run(); err != nil {
		return fmt.Errorf("systemctl daemon-reload: %w", err)
	}
	if err := exec.Command("systemctl", "enable", "--now", "phosphor-daemon").Run(); err != nil {
		return fmt.Errorf("systemctl enable: %w", err)
	}

	return nil
}

// Uninstall stops and removes the systemd service.
func Uninstall() error {
	_ = exec.Command("systemctl", "disable", "--now", "phosphor-daemon").Run()
	if err := os.Remove(serviceUnitPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	_ = exec.Command("systemctl", "daemon-reload").Run()
	return nil
}

// IsServiceEnvironment returns false on Linux — systemd runs us as a normal process.
func IsServiceEnvironment() bool {
	return false
}

// RunAsService is a no-op on Linux. The daemon runs in foreground; systemd manages the lifecycle.
func RunAsService(d *Daemon) error {
	return fmt.Errorf("RunAsService is not used on Linux")
}
