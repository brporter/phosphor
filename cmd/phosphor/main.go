package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/brporter/phosphor/internal/cli"
	"github.com/spf13/cobra"
)

func main() {
	var relayURL string
	var token string

	rootCmd := &cobra.Command{
		Use:   "phosphor [-- command args...]",
		Short: "Share your terminal over the web",
		Long:  "phosphor captures process I/O and streams it through a relay server to a web viewer.",
		Example: `  # PTY mode: wrap a command
  phosphor -- bash
  phosphor -- vim file.txt

  # Pipe mode: pipe stdout
  ping localhost | phosphor
  tail -f /var/log/syslog | phosphor`,
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

			cfg := cli.DefaultConfig()
			if relayURL != "" {
				cfg.RelayURL = relayURL
			}

			// Determine mode
			mode := "pipe"
			var command []string
			if cmd.ArgsLenAtDash() == 0 && len(args) > 0 {
				mode = "pty"
				command = args
			} else if cmd.ArgsLenAtDash() >= 0 {
				// No args after --, that's an error for PTY mode
				if len(args) == 0 {
					// Pipe mode from stdin
					mode = "pipe"
				}
			}
			if len(args) > 0 {
				mode = "pty"
				command = args
			}

			// Load token from flag or cache
			if token == "" {
				if cache, err := cli.LoadTokenCache(); err == nil {
					token = cache.AccessToken
				}
			}

			app := &cli.App{
				Config:  cfg,
				Token:   token,
				Logger:  logger,
				Command: command,
				Mode:    mode,
			}

			return app.Run(context.Background())
		},
	}

	rootCmd.Flags().StringVar(&relayURL, "relay", "", fmt.Sprintf("Relay server URL (default: %s)", cli.DefaultRelayURL))
	rootCmd.Flags().StringVar(&token, "token", "", "Auth token (default: read from cache)")

	var provider string
	var useDeviceCode bool
	loginCmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate with an identity provider",
		RunE: func(cmd *cobra.Command, args []string) error {
			relay := relayURL
			if relay == "" {
				relay = cli.DefaultConfig().RelayURL
			}
			return cli.Login(context.Background(), provider, relay, useDeviceCode)
		},
	}
	loginCmd.Flags().StringVar(&provider, "provider", "microsoft", "OIDC provider (apple, microsoft, google)")
	loginCmd.Flags().BoolVar(&useDeviceCode, "device-code", false, "Use device code flow instead of browser (Microsoft/Google only)")

	logoutCmd := &cobra.Command{
		Use:   "logout",
		Short: "Clear cached authentication tokens",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := cli.ClearTokenCache(); err != nil {
				return err
			}
			fmt.Println("Logged out successfully")
			return nil
		},
	}

	rootCmd.AddCommand(loginCmd, logoutCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
