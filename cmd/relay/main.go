package main

import (
	"context"
	"crypto/rand"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/brporter/phosphor/internal/auth"
	"github.com/brporter/phosphor/internal/relay"
	"github.com/brporter/phosphor/internal/sshgate"
	dbstore "github.com/brporter/phosphor/internal/store"
)

func main() {
	godotenv.Load() // load .env if present; no error if missing

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}

	baseURL := os.Getenv("BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}

	devMode := os.Getenv("DEV_MODE") != ""

	// Durable state (tenants, users, machines, API keys) lives in Postgres.
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		logger.Error("DATABASE_URL is required")
		os.Exit(1)
	}
	db, err := dbstore.New(context.Background(), databaseURL)
	if err != nil {
		logger.Error("database setup failed", "err", err)
		os.Exit(1)
	}
	defer db.Close()
	logger.Info("connected to Postgres, migrations applied")

	// Set up OIDC verifier (providers configured via env vars)
	verifier := auth.NewVerifier(logger)

	// Register providers from env if configured
	ctx := context.Background()
	if clientID := os.Getenv("MICROSOFT_CLIENT_ID"); clientID != "" {
		if err := verifier.AddProvider(ctx, auth.ProviderConfig{
			Name:          "microsoft",
			Issuer:        "https://login.microsoftonline.com/common/v2.0",
			ClientID:      clientID,
			ClientSecret:  os.Getenv("MICROSOFT_CLIENT_SECRET"),
			DeviceAuthURL: "https://login.microsoftonline.com/common/oauth2/v2.0/devicecode",
		}); err != nil {
			logger.Warn("failed to register Microsoft provider", "err", err)
		}
	}
	if clientID := os.Getenv("GOOGLE_CLIENT_ID"); clientID != "" {
		if err := verifier.AddProvider(ctx, auth.ProviderConfig{
			Name:          "google",
			Issuer:        "https://accounts.google.com",
			ClientID:      clientID,
			ClientSecret:  os.Getenv("GOOGLE_CLIENT_SECRET"),
			DeviceAuthURL: "https://oauth2.googleapis.com/device/code",
		}); err != nil {
			logger.Warn("failed to register Google provider", "err", err)
		}
	}
	if clientID := os.Getenv("APPLE_CLIENT_ID"); clientID != "" {
		teamID := os.Getenv("APPLE_TEAM_ID")
		keyID := os.Getenv("APPLE_KEY_ID")
		pkRaw := os.Getenv("APPLE_PRIVATE_KEY")

		if teamID == "" || keyID == "" || pkRaw == "" {
			logger.Warn("APPLE_CLIENT_ID set but missing APPLE_TEAM_ID, APPLE_KEY_ID, or APPLE_PRIVATE_KEY")
		} else {
			privateKey, err := auth.ParseP8PrivateKey([]byte(pkRaw))
			if err != nil {
				logger.Warn("failed to parse Apple private key", "err", err)
			} else {
				if err := verifier.AddProvider(ctx, auth.ProviderConfig{
					Name:       "apple",
					Issuer:     "https://appleid.apple.com",
					ClientID:   clientID,
					TeamID:     teamID,
					KeyID:      keyID,
					PrivateKey: privateKey,
				}); err != nil {
					logger.Warn("failed to register Apple provider", "err", err)
				}
			}
		}
	}

	// Pending OIDC auth flows live in-memory (single-instance deployment).
	authSessions := relay.NewMemoryAuthSessionStore(5 * time.Minute)

	// API key signing secret
	apiKeySecret := []byte(os.Getenv("API_KEY_SECRET"))
	if len(apiKeySecret) == 0 {
		generated := make([]byte, 32)
		rand.Read(generated)
		apiKeySecret = generated
		logger.Warn("API_KEY_SECRET not set — generated random secret; API keys will not survive restarts")
	}

	srv := relay.NewServer(logger, baseURL, verifier, devMode, authSessions, apiKeySecret, db)

	// SSH gateway for CLI reverse tunnels
	sshAddr := os.Getenv("SSH_ADDR")
	if sshAddr == "" {
		sshAddr = ":2222"
	}
	hostKeyFile := os.Getenv("SSH_HOST_KEY_FILE")
	if hostKeyFile == "" {
		hostKeyFile = "/etc/phosphor/ssh_host_key"
	}
	hostKey, err := sshgate.LoadOrCreateHostKey(hostKeyFile)
	if err != nil {
		logger.Error("ssh host key setup failed", "err", err)
		os.Exit(1)
	}
	registry := sshgate.NewRegistry()
	gate := sshgate.NewServer(registry, db, hostKey, logger)
	srv.SetSSHGate(registry, sshPublicAddr(baseURL, sshAddr), hostKey.PublicKey())

	httpServer := &http.Server{
		Addr:         addr,
		Handler:      srv.Handler(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	go func() {
		if err := gate.ListenAndServe(ctx, sshAddr); err != nil {
			logger.Error("ssh gateway error", "err", err)
			cancel()
		}
	}()

	// Dev-only: expose one machine's tunnel on a raw TCP port so a normal
	// `ssh -p <port> localhost` can exercise the tunnel before the browser
	// client exists. Never enabled in production.
	if debugListen := os.Getenv("SSH_DEBUG_LISTEN"); debugListen != "" && devMode {
		debugMachine := os.Getenv("SSH_DEBUG_MACHINE")
		go runDebugListener(ctx, debugListen, debugMachine, registry, logger)
	}

	go func() {
		logger.Info("relay server starting", "addr", addr, "base_url", baseURL, "dev_mode", devMode, "ssh_addr", sshAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "err", err)
			cancel()
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	authSessions.Stop()
	httpServer.Shutdown(shutdownCtx)
}

// sshPublicAddr derives the host:port CLIs should dial for the SSH gateway:
// SSH_PUBLIC_ADDR wins, otherwise the BASE_URL hostname plus the gateway's
// listen port.
func sshPublicAddr(baseURL, sshAddr string) string {
	if pub := os.Getenv("SSH_PUBLIC_ADDR"); pub != "" {
		return pub
	}
	host := "localhost"
	if u, err := url.Parse(baseURL); err == nil && u.Hostname() != "" {
		host = u.Hostname()
	}
	_, port, err := net.SplitHostPort(sshAddr)
	if err != nil || port == "" {
		port = "2222"
	}
	return net.JoinHostPort(host, port)
}

// runDebugListener pipes raw TCP connections into one machine's tunnel so a
// plain ssh client can exercise it during development.
func runDebugListener(ctx context.Context, addr, machineID string, registry *sshgate.Registry, logger *slog.Logger) {
	if machineID == "" {
		logger.Warn("SSH_DEBUG_LISTEN set but SSH_DEBUG_MACHINE is empty; debug listener disabled")
		return
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		logger.Error("debug listener failed", "addr", addr, "err", err)
		return
	}
	go func() {
		<-ctx.Done()
		ln.Close()
	}()
	logger.Warn("SSH DEBUG listener active — raw TCP piped to tunnel", "addr", addr, "machine", machineID)
	for {
		nc, err := ln.Accept()
		if err != nil {
			return
		}
		go func() {
			defer nc.Close()
			tc, err := registry.Dial(machineID)
			if err != nil {
				logger.Warn("debug dial failed", "err", err)
				return
			}
			defer tc.Close()
			done := make(chan struct{}, 2)
			go func() { io.Copy(tc, nc); done <- struct{}{} }()
			go func() { io.Copy(nc, tc); done <- struct{}{} }()
			<-done
		}()
	}
}
