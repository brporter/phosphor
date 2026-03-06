package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/brporter/phosphor/internal/cli"
	"github.com/spf13/cobra"
)

func main() {
	var relayURL string
	var token string
	var restart string
	var logout bool
	var logFile string
	var debug bool

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
			if logout {
				if err := cli.ClearTokenCache(); err != nil {
					return err
				}
				fmt.Println("Logged out successfully")
				return nil
			}

			// Configure logger based on --log and --debug flags
			var logWriter io.Writer
			var logLevel slog.Level
			var logFileHandle *os.File

			switch {
			case logFile != "" && debug:
				f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
				if err != nil {
					return fmt.Errorf("open log file: %w", err)
				}
				logFileHandle = f
				logWriter = io.MultiWriter(f, os.Stderr)
				logLevel = slog.LevelDebug
			case logFile != "":
				f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
				if err != nil {
					return fmt.Errorf("open log file: %w", err)
				}
				logFileHandle = f
				logWriter = f
				logLevel = slog.LevelInfo
			case debug:
				logWriter = os.Stderr
				logLevel = slog.LevelDebug
			default:
				logWriter = io.Discard
				logLevel = slog.LevelInfo
			}

			if logFileHandle != nil {
				defer logFileHandle.Close()
			}

			logger := slog.New(slog.NewTextHandler(logWriter, &slog.HandlerOptions{Level: logLevel}))

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

			if mode == "pipe" {
				stat, _ := os.Stdin.Stat()
				if (stat.Mode() & os.ModeCharDevice) != 0 {
					return fmt.Errorf("no command specified and nothing piped to stdin\n\nUsage:\n  phosphor -- <command>        (e.g. phosphor -- bash)\n  <command> | phosphor          (e.g. ping localhost | phosphor)")
				}
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
				Restart: restart,
			}

			return app.Run(context.Background())
		},
	}

	rootCmd.Flags().StringVar(&relayURL, "relay", "", fmt.Sprintf("Relay server URL (default: %s)", cli.DefaultRelayURL))
	rootCmd.Flags().StringVar(&token, "token", "", "Auth token (default: read from cache)")
	rootCmd.Flags().StringVar(&restart, "restart", "manual", "Process restart mode: manual, auto, never")
	rootCmd.Flags().BoolVar(&logout, "logout", false, "Clear cached authentication tokens and exit")
	rootCmd.Flags().StringVar(&logFile, "log", "", "Write log messages to file")
	rootCmd.Flags().BoolVar(&debug, "debug", false, "Enable debug logging to stderr")

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

	rootCmd.AddCommand(loginCmd, logoutCmd, newDaemonCmd())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
