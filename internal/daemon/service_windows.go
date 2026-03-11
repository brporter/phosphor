//go:build windows

package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const serviceName = "PhosphorDaemon"
const serviceDisplayName = "Phosphor Terminal Sharing Daemon"

// Install creates and starts the Windows service.
func Install(binaryPath string) error {
	if binaryPath == "" {
		var err error
		binaryPath, err = os.Executable()
		if err != nil {
			return fmt.Errorf("resolve executable: %w", err)
		}
		binaryPath, _ = filepath.Abs(binaryPath)
	}

	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to SCM: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err == nil {
		s.Close()
		return fmt.Errorf("service %q already exists", serviceName)
	}

	s, err = m.CreateService(serviceName, binaryPath, mgr.Config{
		DisplayName: serviceDisplayName,
		StartType:   mgr.StartAutomatic,
		Description: "Maintains persistent connections to the Phosphor relay and spawns terminal sessions on demand.",
	}, "daemon", "run")
	if err != nil {
		return fmt.Errorf("create service: %w", err)
	}
	defer s.Close()

	// Configure service recovery: restart on failure with increasing delays.
	recoveryActions := []mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 30 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 60 * time.Second},
	}
	if err := s.SetRecoveryActions(recoveryActions, 3600); err != nil {
		// Non-fatal: log but continue — the service will still work,
		// just without automatic restart on crash.
		fmt.Fprintf(os.Stderr, "warning: could not set recovery actions: %v\n", err)
	}

	if err := s.Start(); err != nil {
		return fmt.Errorf("start service: %w", err)
	}

	return nil
}

// Uninstall stops and removes the Windows service.
func Uninstall() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to SCM: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err != nil {
		return fmt.Errorf("open service: %w", err)
	}
	defer s.Close()

	status, err := s.Control(svc.Stop)
	if err == nil {
		for status.State != svc.Stopped {
			time.Sleep(500 * time.Millisecond)
			status, _ = s.Query()
		}
	}

	return s.Delete()
}

// IsServiceEnvironment returns true if running under the Windows SCM.
func IsServiceEnvironment() bool {
	isService, _ := svc.IsWindowsService()
	return isService
}

// phosphorService implements svc.Handler for the Windows SCM.
type phosphorService struct {
	daemon *Daemon
}

func (ps *phosphorService) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	changes <- svc.Status{State: svc.StartPending}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		ps.daemon.Run(ctx)
		close(done)
	}()

	changes <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}

	for {
		select {
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				changes <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				changes <- svc.Status{State: svc.StopPending}
				cancel()
				<-done
				return false, 0
			}
		case <-done:
			// daemon.Run() returned without being asked to stop —
			// exit with non-zero code to trigger SCM recovery (auto-restart).
			return false, 1
		}
	}
}

// RunAsService runs the daemon under the Windows SCM.
func RunAsService(d *Daemon) error {
	return svc.Run(serviceName, &phosphorService{daemon: d})
}
