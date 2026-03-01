package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	gonanoid "github.com/matoous/go-nanoid/v2"
	"github.com/redis/go-redis/v9"

	"github.com/brporter/phosphor/internal/auth"
	"github.com/brporter/phosphor/internal/relay"
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

	// Generate unique relay instance ID
	relayID, _ := gonanoid.New(12)

	// Set up backends based on REDIS_URL
	var (
		store        relay.SessionStore
		bus          relay.MessageBus
		authSessions relay.AuthSessionStoreI
	)

	if redisURL := os.Getenv("REDIS_URL"); redisURL != "" {
		opts, err := redis.ParseURL(redisURL)
		if err != nil {
			logger.Error("invalid REDIS_URL", "err", err)
			os.Exit(1)
		}
		rdb := redis.NewClient(opts)

		if err := rdb.Ping(ctx).Err(); err != nil {
			logger.Error("redis ping failed", "err", err)
			os.Exit(1)
		}
		logger.Info("connected to Redis", "url", redisURL)

		rs := relay.NewRedisSessionStore(rdb, logger)
		rs.SetExpiryCallback(nil) // wired after hub creation below
		store = rs
		bus = relay.NewRedisMessageBus(rdb, logger)
		authSessions = relay.NewRedisAuthSessionStore(rdb)
	} else {
		ms := relay.NewMemorySessionStore()
		store = ms
		bus = nil
		authSessions = relay.NewMemoryAuthSessionStore(5 * time.Minute)

		// Wire expiry callback after hub creation (deferred below)
		defer func() {
			// This is set after hub is created via the closure
		}()
	}

	hub := relay.NewHub(store, bus, relayID, logger)

	// Wire expiry callbacks now that hub exists
	if ms, ok := store.(*relay.MemorySessionStore); ok {
		ms.SetExpiryCallback(func(ctx context.Context, id string) {
			hub.Unregister(ctx, id)
			logger.Info("grace period expired, session removed", "id", id)
		})
	}
	if rs, ok := store.(*relay.RedisSessionStore); ok {
		rs.SetExpiryCallback(func(ctx context.Context, id string) {
			hub.Unregister(ctx, id)
			logger.Info("grace period expired, session removed", "id", id)
		})
		rs.StartExpiryPoller(ctx, 10*time.Second)
	}

	srv := relay.NewServer(hub, logger, baseURL, verifier, devMode, authSessions)

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
		logger.Info("relay server starting", "addr", addr, "base_url", baseURL, "dev_mode", devMode, "relay_id", relayID, "redis", os.Getenv("REDIS_URL") != "")
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
	hub.CloseAll()
	httpServer.Shutdown(shutdownCtx)
}
