package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/brporter/phosphor/internal/auth"
	"github.com/brporter/phosphor/internal/relay"
)

func main() {
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

	hub := relay.NewHub(logger)
	srv := relay.NewServer(hub, logger, baseURL, verifier, devMode)

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
		logger.Info("relay server starting", "addr", addr, "base_url", baseURL, "dev_mode", devMode)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "err", err)
			cancel()
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	hub.CloseAll()
	httpServer.Shutdown(shutdownCtx)
}
