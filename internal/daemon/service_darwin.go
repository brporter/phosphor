//go:build darwin

package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const plistPath = "/Library/LaunchDaemons/com.phosphor.daemon.plist"

// Install creates and loads the launchd plist.
func Install(binaryPath string) error {
	if binaryPath == "" {
		var err error
		binaryPath, err = os.Executable()
		if err != nil {
			return fmt.Errorf("resolve executable: %w", err)
		}
		binaryPath, _ = filepath.Abs(binaryPath)
	}

	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.phosphor.daemon</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
        <string>daemon</string>
        <string>run</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>/var/log/phosphor-daemon.log</string>
    <key>StandardErrorPath</key>
    <string>/var/log/phosphor-daemon.log</string>
</dict>
</plist>
`, binaryPath)

	if err := os.WriteFile(plistPath, []byte(plist), 0644); err != nil {
		return fmt.Errorf("write plist: %w", err)
	}
	if err := exec.Command("launchctl", "load", plistPath).Run(); err != nil {
		return fmt.Errorf("launchctl load: %w", err)
	}
	return nil
}

// Uninstall unloads and removes the launchd plist.
func Uninstall() error {
	_ = exec.Command("launchctl", "unload", plistPath).Run()
	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// IsServiceEnvironment returns false on macOS — launchd runs us as a normal process.
func IsServiceEnvironment() bool {
	return false
}

// RunAsService is a no-op on macOS. The daemon runs in foreground; launchd manages the lifecycle.
func RunAsService(d *Daemon) error {
	return fmt.Errorf("RunAsService is not used on macOS")
}
