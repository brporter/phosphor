package sshgate

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// ErrNotConnected is returned by Dial when the machine has no active tunnel.
var ErrNotConnected = errors.New("machine has no active tunnel")

// Tunnel is one CLI's reverse tunnel: an authenticated SSH connection that
// has requested tcpip-forward for its local sshd.
type Tunnel struct {
	MachineID string
	TenantID  string
	conn      *ssh.ServerConn
	// bindAddr/bindPort echo what the CLI passed to client.Listen; the
	// client's forward mux matches forwarded-tcpip channel opens on them.
	bindAddr string
	bindPort uint32
}

// Registry tracks which machines currently have live tunnels.
type Registry struct {
	mu      sync.RWMutex
	tunnels map[string]*Tunnel
}

func NewRegistry() *Registry {
	return &Registry{tunnels: make(map[string]*Tunnel)}
}

// register adds a tunnel, replacing (and closing) any existing tunnel for
// the same machine — a new connection wins over a possibly-zombie old one.
func (r *Registry) register(t *Tunnel) {
	r.mu.Lock()
	old := r.tunnels[t.MachineID]
	r.tunnels[t.MachineID] = t
	r.mu.Unlock()
	if old != nil && old.conn != t.conn {
		old.conn.Close()
	}
}

// unregister removes a tunnel only if the given connection still owns the
// entry (a replacement tunnel must not be removed by the old conn's cleanup).
func (r *Registry) unregister(machineID string, conn *ssh.ServerConn) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if t, ok := r.tunnels[machineID]; ok && t.conn == conn {
		delete(r.tunnels, machineID)
	}
}

// Online reports whether a machine currently has a tunnel.
func (r *Registry) Online(machineID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.tunnels[machineID]
	return ok
}

// Close terminates a machine's tunnel if one exists.
func (r *Registry) Close(machineID string) bool {
	r.mu.Lock()
	t, ok := r.tunnels[machineID]
	if ok {
		delete(r.tunnels, machineID)
	}
	r.mu.Unlock()
	if ok {
		t.conn.Close()
	}
	return ok
}

// forwardedTCPPayload is the RFC 4254 7.2 forwarded-tcpip channel open payload.
type forwardedTCPPayload struct {
	Addr       string
	Port       uint32
	OriginAddr string
	OriginPort uint32
}

// Dial opens a new forwarded-tcpip channel down the machine's tunnel. The
// CLI accepts it and connects it to the host's sshd, so the returned
// net.Conn carries a fresh TCP-equivalent stream to that sshd. Every
// browser session gets its own channel; SSH multiplexing runs them all over
// one tunnel.
func (r *Registry) Dial(machineID string) (net.Conn, error) {
	r.mu.RLock()
	t, ok := r.tunnels[machineID]
	r.mu.RUnlock()
	if !ok {
		return nil, ErrNotConnected
	}

	// The origin fields are informational, but the x/crypto/ssh client
	// requires a parseable IP and a nonzero port.
	payload := ssh.Marshal(forwardedTCPPayload{
		Addr:       t.bindAddr,
		Port:       t.bindPort,
		OriginAddr: "127.0.0.1",
		OriginPort: 1,
	})
	ch, reqs, err := t.conn.OpenChannel("forwarded-tcpip", payload)
	if err != nil {
		return nil, fmt.Errorf("opening channel to %s: %w", machineID, err)
	}
	go ssh.DiscardRequests(reqs)
	return &channelConn{Channel: ch}, nil
}

// channelConn adapts an ssh.Channel to net.Conn.
type channelConn struct {
	ssh.Channel
}

type tunnelAddr struct{}

func (tunnelAddr) Network() string { return "ssh-tunnel" }
func (tunnelAddr) String() string  { return "ssh-tunnel" }

func (c *channelConn) LocalAddr() net.Addr                { return tunnelAddr{} }
func (c *channelConn) RemoteAddr() net.Addr               { return tunnelAddr{} }
func (c *channelConn) SetDeadline(t time.Time) error      { return nil }
func (c *channelConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *channelConn) SetWriteDeadline(t time.Time) error { return nil }
