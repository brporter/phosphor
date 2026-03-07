package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"testing"
	"time"
)

func TestDaemonStartStop(t *testing.T) {
	d := &Daemon{
		Config: &Config{
			Relay: "ws://localhost:0", // won't connect
			Mappings: []Mapping{
				{Identity: "test@example.com", LocalUser: "test", Shell: "/bin/bash"},
			},
		},
		Logger: slog.Default(),
		Token:  "test-token",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// Should exit when context is cancelled, not panic
	d.Run(ctx)
}

func TestDaemonMultipleMappings(t *testing.T) {
	d := &Daemon{
		Config: &Config{
			Relay: "ws://localhost:0",
			Mappings: []Mapping{
				{Identity: "alice@example.com", LocalUser: "alice", Shell: "/bin/bash"},
				{Identity: "bob@example.com", LocalUser: "bob", Shell: "/bin/zsh"},
			},
		},
		Logger: slog.Default(),
		Token:  "test-token",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	d.Run(ctx)

	// After Run returns, sessions map should have entries
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.sessions) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(d.sessions))
	}
	if _, ok := d.sessions["alice@example.com"]; !ok {
		t.Error("expected session for alice@example.com")
	}
	if _, ok := d.sessions["bob@example.com"]; !ok {
		t.Error("expected session for bob@example.com")
	}
}

func TestGetSetSession(t *testing.T) {
	d := &Daemon{
		Config: &Config{Relay: "ws://localhost:0"},
		Logger: slog.Default(),
		Token:  "test-token",
	}
	d.sessions = make(map[string]*managedSession)

	ms := &managedSession{
		mapping: Mapping{Identity: "test@example.com", LocalUser: "test", Shell: "/bin/bash"},
	}
	d.setSession("test@example.com", ms)

	got := d.getSession("test@example.com")
	if got != ms {
		t.Error("expected to get back the same managed session")
	}

	missing := d.getSession("missing@example.com")
	if missing != nil {
		t.Error("expected nil for missing session")
	}
}

func TestForwardStdinNoProc(t *testing.T) {
	d := &Daemon{
		Config: &Config{Relay: "ws://localhost:0"},
		Logger: slog.Default(),
		Token:  "test-token",
	}
	d.sessions = make(map[string]*managedSession)

	ms := &managedSession{
		mapping: Mapping{Identity: "test@example.com"},
	}
	d.setSession("test@example.com", ms)

	// Should not panic even with no stdinCh
	d.forwardStdin("test@example.com", []byte("hello"))
	d.forwardStdin("missing@example.com", []byte("hello"))
}

func TestIsAuthError(t *testing.T) {
	tests := []struct {
		err  error
		want bool
	}{
		{fmt.Errorf("server error: auth_failed: bad token"), true},
		{fmt.Errorf("server error: invalid_token: expired"), true},
		{fmt.Errorf("dial ws://localhost:8080/ws/cli: connection refused"), false},
		{fmt.Errorf("receive welcome: EOF"), false},
		{nil, false},
	}
	for _, tt := range tests {
		got := isAuthError(tt.err)
		if got != tt.want {
			t.Errorf("isAuthError(%v) = %v, want %v", tt.err, got, tt.want)
		}
	}
}

func TestResizePTYNoProc(t *testing.T) {
	d := &Daemon{
		Config: &Config{Relay: "ws://localhost:0"},
		Logger: slog.Default(),
		Token:  "test-token",
	}
	d.sessions = make(map[string]*managedSession)

	ms := &managedSession{
		mapping: Mapping{Identity: "test@example.com"},
	}
	d.setSession("test@example.com", ms)

	// Should not panic even with no proc
	d.resizePTY("test@example.com", 80, 24)
	d.resizePTY("missing@example.com", 80, 24)
}
