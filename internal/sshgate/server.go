// Package sshgate implements the SSH server that phosphor CLIs connect to
// with reverse tunnels. It never sees terminal plaintext: browser sessions
// are piped through tunnels as opaque byte streams, and the SSH handshake
// that protects them happens end-to-end between the browser and the host's
// own sshd.
package sshgate

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/ssh"

	"github.com/brporter/phosphor/internal/store"
)

// idleTimeout closes tunnels that stop sending traffic (the CLI keepalives
// every 30s, so a healthy tunnel never trips this).
const idleTimeout = 90 * time.Second

// MachineStore is the store surface the gateway needs.
type MachineStore interface {
	GetMachineByFingerprint(ctx context.Context, fingerprint string) (*store.Machine, error)
	TouchMachine(ctx context.Context, id uuid.UUID) error
}

// Server accepts CLI reverse-tunnel connections.
type Server struct {
	registry *Registry
	db       MachineStore
	logger   *slog.Logger
	cfg      *ssh.ServerConfig
	hostKey  ssh.Signer

	mu       sync.Mutex
	listener net.Listener
}

// NewServer creates the gateway. Machines authenticate with the SSH public
// key they registered at enrollment; the key's SHA256 fingerprint is the
// lookup credential and the SSH username must match the machine ID it maps
// to.
func NewServer(registry *Registry, db MachineStore, hostKey ssh.Signer, logger *slog.Logger) *Server {
	s := &Server{registry: registry, db: db, logger: logger, hostKey: hostKey}
	s.cfg = &ssh.ServerConfig{
		ServerVersion: "SSH-2.0-Phosphor",
		MaxAuthTries:  3,
		PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			fingerprint := ssh.FingerprintSHA256(key)
			m, err := db.GetMachineByFingerprint(context.Background(), fingerprint)
			if err != nil {
				return nil, fmt.Errorf("unknown machine key")
			}
			if conn.User() != m.ID.String() {
				return nil, fmt.Errorf("machine key does not match user %q", conn.User())
			}
			return &ssh.Permissions{Extensions: map[string]string{
				"machine-id": m.ID.String(),
				"tenant-id":  m.TenantID.String(),
			}}, nil
		},
	}
	s.cfg.AddHostKey(hostKey)
	return s
}

// HostPublicKey returns the gateway's public host key for out-of-band
// pinning by CLIs (served over TLS at /api/ssh-info).
func (s *Server) HostPublicKey() ssh.PublicKey {
	return s.hostKey.PublicKey()
}

// ListenAndServe accepts connections until ctx is cancelled.
func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", addr, err)
	}
	s.mu.Lock()
	s.listener = ln
	s.mu.Unlock()

	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	s.logger.Info("ssh gateway listening", "addr", addr)
	for {
		nc, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				continue
			}
			return err
		}
		go s.handleConn(ctx, nc)
	}
}

// Addr returns the listener address (useful with ":0" in tests).
func (s *Server) Addr() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener == nil {
		return nil
	}
	return s.listener.Addr()
}

func (s *Server) handleConn(ctx context.Context, nc net.Conn) {
	ic := &idleConn{Conn: nc, timeout: idleTimeout}
	ic.touch()

	conn, chans, reqs, err := ssh.NewServerConn(ic, s.cfg)
	if err != nil {
		s.logger.Debug("ssh handshake failed", "remote", nc.RemoteAddr(), "err", err)
		nc.Close()
		return
	}
	machineID := conn.Permissions.Extensions["machine-id"]
	tenantID := conn.Permissions.Extensions["tenant-id"]
	logger := s.logger.With("machine", machineID, "remote", nc.RemoteAddr())
	logger.Info("tunnel connected")

	if id, err := uuid.Parse(machineID); err == nil {
		if err := s.db.TouchMachine(ctx, id); err != nil {
			logger.Warn("touching machine on connect", "err", err)
		}
	}

	// CLIs never open channels; hosts are reached only by the gateway
	// opening forwarded-tcpip channels in Registry.Dial.
	go func() {
		for newChan := range chans {
			newChan.Reject(ssh.Prohibited, "phosphor CLIs do not open channels")
		}
	}()

	go func() {
		defer func() {
			s.registry.unregister(machineID, conn)
			if id, err := uuid.Parse(machineID); err == nil {
				if err := s.db.TouchMachine(context.Background(), id); err != nil {
					logger.Warn("touching machine on disconnect", "err", err)
				}
			}
			logger.Info("tunnel disconnected")
		}()

		for req := range reqs {
			switch req.Type {
			case "tcpip-forward":
				var payload struct {
					Addr string
					Port uint32
				}
				if err := ssh.Unmarshal(req.Payload, &payload); err != nil {
					req.Reply(false, nil)
					continue
				}
				s.registry.register(&Tunnel{
					MachineID: machineID,
					TenantID:  tenantID,
					conn:      conn,
					bindAddr:  payload.Addr,
					bindPort:  payload.Port,
				})
				logger.Info("tunnel forwarding registered", "addr", payload.Addr, "port", payload.Port)
				req.Reply(true, nil)
			case "cancel-tcpip-forward":
				s.registry.unregister(machineID, conn)
				req.Reply(true, nil)
			case "keepalive@openssh.com":
				req.Reply(true, nil)
			default:
				if req.WantReply {
					req.Reply(false, nil)
				}
			}
		}
	}()

	go func() {
		conn.Wait()
		nc.Close()
	}()
}

// idleConn extends a read deadline on every successful read or write, so a
// silent (dead) tunnel is closed after idleTimeout.
type idleConn struct {
	net.Conn
	timeout time.Duration
}

func (c *idleConn) touch() {
	c.Conn.SetReadDeadline(time.Now().Add(c.timeout))
}

func (c *idleConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 {
		c.touch()
	}
	return n, err
}

func (c *idleConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	if n > 0 {
		c.touch()
	}
	return n, err
}
