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

	rootCmd := &cobra.Command{
		Use:   "phosphor",
		Short: "Access your machines from the browser over SSH tunnels",
		Long: `phosphor exposes a machine's SSH daemon to the Phosphor relay over a
reverse tunnel. Users connect from the Phosphor web app, which runs an
SSH client in the browser end-to-end to the machine — the relay only
ever pipes ciphertext.

Typical setup on a machine you want to reach:
  phosphor enroll --relay https://your-relay-server
  phosphor tunnel`,
	}

	rootCmd.PersistentFlags().StringVar(&relayURL, "relay", "", "Relay server URL")

	resolveRelay := func() (string, error) {
		relay := relayURL
		if relay == "" {
			relay = cli.DefaultConfig().RelayURL
		}
		if relay == "" {
			return "", fmt.Errorf("--relay flag is required (e.g. --relay https://your-relay-server)")
		}
		return relay, nil
	}

	// --- login ---
	var provider string
	var useDeviceCode bool
	loginCmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate with an identity provider",
		RunE: func(cmd *cobra.Command, args []string) error {
			relay, err := resolveRelay()
			if err != nil {
				return err
			}
			return cli.Login(context.Background(), provider, relay, useDeviceCode)
		},
	}
	loginCmd.Flags().StringVar(&provider, "provider", "microsoft", "OIDC provider (apple, microsoft, google)")
	loginCmd.Flags().BoolVar(&useDeviceCode, "device-code", false, "Use device code flow instead of browser (Microsoft/Google only)")

	// --- logout ---
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

	// --- enroll ---
	var enrollName string
	var enrollAPIKey string
	var enrollSSHDAddr string
	enrollCmd := &cobra.Command{
		Use:   "enroll",
		Short: "Register this machine with the relay for SSH tunnel access",
		Long:  "Generates a machine keypair, registers it under your account, and pins the relay's SSH gateway endpoint. Run once per machine, then use `phosphor tunnel`.",
		RunE: func(cmd *cobra.Command, args []string) error {
			relay, err := resolveRelay()
			if err != nil {
				return err
			}
			cfg, err := cli.Enroll(context.Background(), cli.EnrollOptions{
				RelayURL: relay,
				Name:     enrollName,
				APIKey:   enrollAPIKey,
				SSHDAddr: enrollSSHDAddr,
			})
			if err != nil {
				return err
			}
			fmt.Printf("Machine enrolled.\n  id:      %s\n  gateway: %s\n\nStart the tunnel with: phosphor tunnel\n", cfg.MachineID, cfg.SSHAddr)
			return nil
		},
	}
	enrollCmd.Flags().StringVar(&enrollName, "name", "", "Machine display name (default: hostname)")
	enrollCmd.Flags().StringVar(&enrollAPIKey, "api-key", "", "API key (phk:...) for headless enrollment")
	enrollCmd.Flags().StringVar(&enrollSSHDAddr, "sshd-addr", "", "Local sshd address the tunnel exposes (default 127.0.0.1:22)")

	// --- tunnel ---
	var tunnelSSHDAddr string
	var tunnelDebug bool
	tunnelCmd := &cobra.Command{
		Use:   "tunnel",
		Short: "Maintain a reverse SSH tunnel to the relay",
		Long:  "Connects to the relay's SSH gateway and exposes this machine's sshd through the tunnel. Reconnects automatically. Requires `phosphor enroll` first.\n\nTo run as a service, wrap this command in systemd/launchd (see docs/DEPLOYMENT.md).",
		RunE: func(cmd *cobra.Command, args []string) error {
			machine, err := cli.LoadMachineConfig()
			if err != nil {
				return fmt.Errorf("no machine enrollment found — run `phosphor enroll` first (%w)", err)
			}
			signer, err := cli.LoadMachineKey()
			if err != nil {
				return fmt.Errorf("loading machine key: %w", err)
			}
			level := slog.LevelInfo
			if tunnelDebug {
				level = slog.LevelDebug
			}
			logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
			return cli.RunTunnel(context.Background(), cli.TunnelOptions{
				Machine:  machine,
				Signer:   signer,
				Logger:   logger,
				SSHDAddr: tunnelSSHDAddr,
			})
		},
	}
	tunnelCmd.Flags().StringVar(&tunnelSSHDAddr, "sshd-addr", "", "Local sshd address the tunnel exposes (default from enrollment, else 127.0.0.1:22)")
	tunnelCmd.Flags().BoolVar(&tunnelDebug, "debug", false, "Enable debug logging")

	rootCmd.AddCommand(loginCmd, logoutCmd, enrollCmd, tunnelCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
