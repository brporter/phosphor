package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

const (
	keepaliveInterval = 30 * time.Second
	keepaliveTimeout  = 15 * time.Second
	maxBackoff        = 60 * time.Second
	defaultSSHDAddr   = "127.0.0.1:22"
)

// TunnelOptions configures the reverse tunnel loop.
type TunnelOptions struct {
	Machine  *MachineConfig
	Signer   ssh.Signer
	Logger   *slog.Logger
	SSHDAddr string // overrides Machine.SSHDAddr
}

// RunTunnel maintains a reverse tunnel to the gateway until ctx is
// cancelled, reconnecting with exponential backoff (this is what survives
// relay redeploys). Each forwarded-tcpip channel the gateway opens becomes a
// fresh connection to the local sshd.
func RunTunnel(ctx context.Context, opts TunnelOptions) error {
	sshdAddr := opts.SSHDAddr
	if sshdAddr == "" {
		sshdAddr = opts.Machine.SSHDAddr
	}
	if sshdAddr == "" {
		sshdAddr = defaultSSHDAddr
	}

	hostKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(opts.Machine.HostKey))
	if err != nil {
		return fmt.Errorf("parsing pinned gateway host key: %w", err)
	}

	backoff := time.Second
	for {
		err := runTunnelOnce(ctx, opts, hostKey, sshdAddr)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			opts.Logger.Warn("tunnel disconnected", "err", err)
		}

		// Jittered exponential backoff.
		delay := backoff/2 + time.Duration(rand.Int63n(int64(backoff)))
		opts.Logger.Info("reconnecting", "in", delay.Round(time.Millisecond))
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(delay):
		}
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

func runTunnelOnce(ctx context.Context, opts TunnelOptions, hostKey ssh.PublicKey, sshdAddr string) error {
	cfg := &ssh.ClientConfig{
		User:            opts.Machine.MachineID,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(opts.Signer)},
		HostKeyCallback: ssh.FixedHostKey(hostKey),
		Timeout:         15 * time.Second,
	}

	client, err := ssh.Dial("tcp", opts.Machine.SSHAddr, cfg)
	if err != nil {
		return fmt.Errorf("dialing gateway %s: %w", opts.Machine.SSHAddr, err)
	}
	defer client.Close()

	// The address is symbolic — the gateway never binds it; it only echoes
	// it back when opening forwarded-tcpip channels.
	listener, err := client.Listen("tcp", "0.0.0.0:22")
	if err != nil {
		return fmt.Errorf("requesting reverse forward: %w", err)
	}
	defer listener.Close()

	opts.Logger.Info("tunnel established", "gateway", opts.Machine.SSHAddr, "exposing", sshdAddr)

	connCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Keepalives detect dead gateways; a missed reply tears the tunnel down
	// so the backoff loop can rebuild it.
	go func() {
		ticker := time.NewTicker(keepaliveInterval)
		defer ticker.Stop()
		for {
			select {
			case <-connCtx.Done():
				return
			case <-ticker.C:
				done := make(chan error, 1)
				go func() {
					_, _, err := client.SendRequest("keepalive@openssh.com", true, nil)
					done <- err
				}()
				select {
				case err := <-done:
					if err != nil {
						opts.Logger.Debug("keepalive failed", "err", err)
						client.Close()
						return
					}
				case <-time.After(keepaliveTimeout):
					opts.Logger.Debug("keepalive timed out")
					client.Close()
					return
				case <-connCtx.Done():
					return
				}
			}
		}
	}()

	go func() {
		<-connCtx.Done()
		client.Close()
	}()

	var wg sync.WaitGroup
	for {
		ch, err := listener.Accept()
		if err != nil {
			wg.Wait()
			if errors.Is(err, io.EOF) || connCtx.Err() != nil {
				return nil
			}
			return err
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			bridgeToSSHD(connCtx, ch, sshdAddr, opts.Logger)
		}()
	}
}

// bridgeToSSHD connects one forwarded channel to the local sshd.
func bridgeToSSHD(ctx context.Context, ch net.Conn, sshdAddr string, logger *slog.Logger) {
	defer ch.Close()

	var d net.Dialer
	local, err := d.DialContext(ctx, "tcp", sshdAddr)
	if err != nil {
		logger.Warn("connecting to local sshd", "addr", sshdAddr, "err", err)
		return
	}
	defer local.Close()

	logger.Debug("session opened")
	done := make(chan struct{}, 2)
	go func() {
		io.Copy(local, ch)
		if hc, ok := local.(interface{ CloseWrite() error }); ok {
			hc.CloseWrite()
		}
		done <- struct{}{}
	}()
	go func() {
		io.Copy(ch, local)
		done <- struct{}{}
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}
	logger.Debug("session closed")
}
