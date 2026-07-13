package sshgate_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/brporter/phosphor/internal/cli"
	"github.com/brporter/phosphor/internal/sshgate"
	"github.com/brporter/phosphor/internal/store"
)

// startEchoServer stands in for the host's sshd: the tunnel is a transparent
// byte pipe, so echoing proves end-to-end transport.
func startEchoServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				io.Copy(c, c)
			}()
		}
	}()
	return ln.Addr().String()
}

type testEnv struct {
	registry  *sshgate.Registry
	db        *store.Fake
	gateAddr  string
	machineID string
	signer    ssh.Signer
	hostKey   string
}

func startGateway(t *testing.T) *testEnv {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	db := store.NewFake()
	user, err := db.GetOrCreateUser(ctx, "google", "sub1", "alice@example.com")
	if err != nil {
		t.Fatal(err)
	}

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	machine, err := db.CreateMachine(ctx, user.TenantID, "testbox", "testbox.local", ssh.FingerprintSHA256(signer.PublicKey()))
	if err != nil {
		t.Fatal(err)
	}

	_, hostPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	hostSigner, err := ssh.NewSignerFromKey(hostPriv)
	if err != nil {
		t.Fatal(err)
	}

	registry := sshgate.NewRegistry()
	gate := sshgate.NewServer(registry, db, hostSigner, slog.Default())

	started := make(chan struct{})
	go func() {
		close(started)
		gate.ListenAndServe(ctx, "127.0.0.1:0")
	}()
	<-started
	var addr net.Addr
	for i := 0; i < 100; i++ {
		if addr = gate.Addr(); addr != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if addr == nil {
		t.Fatal("gateway did not start")
	}

	return &testEnv{
		registry:  registry,
		db:        db,
		gateAddr:  addr.String(),
		machineID: machine.ID.String(),
		signer:    signer,
		hostKey:   strings.TrimSpace(string(ssh.MarshalAuthorizedKey(hostSigner.PublicKey()))),
	}
}

func waitOnline(t *testing.T, env *testEnv, want bool, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if env.registry.Online(env.machineID) == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("machine online != %v after %v", want, timeout)
}

func startTunnel(t *testing.T, env *testEnv, sshdAddr string) context.CancelFunc {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	go cli.RunTunnel(ctx, cli.TunnelOptions{
		Machine: &cli.MachineConfig{
			MachineID: env.machineID,
			SSHAddr:   env.gateAddr,
			HostKey:   env.hostKey,
			SSHDAddr:  sshdAddr,
		},
		Signer: env.signer,
		Logger: slog.Default(),
	})
	t.Cleanup(cancel)
	return cancel
}

func TestTunnelEndToEnd(t *testing.T) {
	env := startGateway(t)
	echoAddr := startEchoServer(t)
	startTunnel(t, env, echoAddr)
	waitOnline(t, env, true, 5*time.Second)

	// Multiple concurrent sessions over one tunnel.
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			conn, err := env.registry.Dial(env.machineID)
			if err != nil {
				t.Errorf("Dial %d: %v", i, err)
				return
			}
			defer conn.Close()

			msg := fmt.Sprintf("hello through the tunnel %d", i)
			if _, err := conn.Write([]byte(msg)); err != nil {
				t.Errorf("Write %d: %v", i, err)
				return
			}
			buf := make([]byte, len(msg))
			if _, err := io.ReadFull(conn, buf); err != nil {
				t.Errorf("Read %d: %v", i, err)
				return
			}
			if string(buf) != msg {
				t.Errorf("echo %d = %q, want %q", i, buf, msg)
			}
		}(i)
	}
	wg.Wait()
}

func TestTunnelReconnect(t *testing.T) {
	env := startGateway(t)
	echoAddr := startEchoServer(t)
	startTunnel(t, env, echoAddr)
	waitOnline(t, env, true, 5*time.Second)

	// Kill the tunnel server-side; the CLI's backoff loop must rebuild it.
	if !env.registry.Close(env.machineID) {
		t.Fatal("expected an active tunnel to close")
	}
	waitOnline(t, env, true, 10*time.Second)

	conn, err := env.registry.Dial(env.machineID)
	if err != nil {
		t.Fatalf("Dial after reconnect: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatalf("Write after reconnect: %v", err)
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("Read after reconnect: %v", err)
	}
}

func TestUnknownKeyRejected(t *testing.T) {
	env := startGateway(t)

	_, rogue, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rogueSigner, err := ssh.NewSignerFromKey(rogue)
	if err != nil {
		t.Fatal(err)
	}

	_, err = ssh.Dial("tcp", env.gateAddr, &ssh.ClientConfig{
		User:            env.machineID,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(rogueSigner)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	})
	if err == nil {
		t.Fatal("expected auth failure for unenrolled key")
	}
}

func TestWrongUserRejected(t *testing.T) {
	env := startGateway(t)

	// Correct key, wrong SSH username (must be the machine ID).
	_, err := ssh.Dial("tcp", env.gateAddr, &ssh.ClientConfig{
		User:            "someone-else",
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(env.signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	})
	if err == nil {
		t.Fatal("expected auth failure for mismatched user")
	}
}

func TestDialOffline(t *testing.T) {
	env := startGateway(t)
	if _, err := env.registry.Dial(env.machineID); err == nil {
		t.Fatal("expected error dialing offline machine")
	}
}
