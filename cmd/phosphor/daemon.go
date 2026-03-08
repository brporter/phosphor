package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/brporter/phosphor/internal/daemon"
)

func newDaemonCmd() *cobra.Command {
	var configPath string

	daemonCmd := &cobra.Command{
		Use:   "daemon",
		Short: "Manage the phosphor background daemon",
	}

	// --- daemon run ---
	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Run the daemon (foreground or as a service)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if configPath == "" {
				configPath = daemon.DefaultConfigPath()
			}

			cfg, err := daemon.ReadConfig(configPath)
			if err != nil {
				return fmt.Errorf("no config at %s — run 'phosphor daemon map' first: %w", configPath, err)
			}
			if len(cfg.Mappings) == 0 {
				return fmt.Errorf("no mappings configured — run 'phosphor daemon map' first")
			}

			logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

			if cfg.ApiKey == "" {
				return fmt.Errorf("no api_key in config — generate one in the web UI and run 'phosphor daemon set-key <key>'")
			}

			d := &daemon.Daemon{
				Config:     cfg,
				Token:      cfg.ApiKey,
				Logger:     logger,
				ConfigPath: configPath,
				Spawn:      daemon.StartPTYAsUser,
			}

			if daemon.IsServiceEnvironment() {
				return daemon.RunAsService(d)
			}

			ctx, cancel := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			d.Run(ctx)
			return nil
		},
	}

	// --- daemon install ---
	installCmd := &cobra.Command{
		Use:   "install",
		Short: "Install the daemon as a system service",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := daemon.Install(""); err != nil {
				return err
			}
			fmt.Println("Phosphor daemon installed and started.")
			return nil
		},
	}

	// --- daemon uninstall ---
	uninstallCmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the daemon system service",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := daemon.Uninstall(); err != nil {
				return err
			}
			fmt.Println("Phosphor daemon uninstalled.")
			return nil
		},
	}

	// --- daemon map ---
	var mapIdentity, mapUser, mapShell, mapRelay string
	mapCmd := &cobra.Command{
		Use:   "map",
		Short: "Map a web identity to a local user account",
		RunE: func(cmd *cobra.Command, args []string) error {
			if configPath == "" {
				configPath = daemon.DefaultConfigPath()
			}
			cfg, err := daemon.ReadConfig(configPath)
			if err != nil {
				if mapRelay == "" {
					return fmt.Errorf("--relay flag is required when creating a new config (e.g. --relay wss://your-relay-server)")
				}
				cfg = &daemon.Config{Relay: mapRelay}
			}
			if mapRelay != "" {
				cfg.Relay = mapRelay
			}
			cfg.AddMapping(daemon.Mapping{
				Identity:  mapIdentity,
				LocalUser: mapUser,
				Shell:     mapShell,
			})
			if err := daemon.WriteConfig(configPath, cfg); err != nil {
				return err
			}
			fmt.Printf("Mapped %s → %s (%s)\n", mapIdentity, mapUser, mapShell)
			return nil
		},
	}
	mapCmd.Flags().StringVar(&mapIdentity, "identity", "", "Web identity (email)")
	mapCmd.Flags().StringVar(&mapUser, "user", "", "Local user account")
	mapCmd.Flags().StringVar(&mapShell, "shell", "", "Shell to launch")
	mapCmd.Flags().StringVar(&mapRelay, "relay", "", "Relay URL (used when creating new config)")
	mapCmd.MarkFlagRequired("identity")
	mapCmd.MarkFlagRequired("user")
	mapCmd.MarkFlagRequired("shell")

	// --- daemon unmap ---
	var unmapIdentity string
	unmapCmd := &cobra.Command{
		Use:   "unmap",
		Short: "Remove an identity mapping",
		RunE: func(cmd *cobra.Command, args []string) error {
			if configPath == "" {
				configPath = daemon.DefaultConfigPath()
			}
			cfg, err := daemon.ReadConfig(configPath)
			if err != nil {
				return err
			}
			if !cfg.RemoveMapping(unmapIdentity) {
				return fmt.Errorf("no mapping for %q", unmapIdentity)
			}
			if err := daemon.WriteConfig(configPath, cfg); err != nil {
				return err
			}
			fmt.Printf("Removed mapping for %s\n", unmapIdentity)
			return nil
		},
	}
	unmapCmd.Flags().StringVar(&unmapIdentity, "identity", "", "Web identity to remove")
	unmapCmd.MarkFlagRequired("identity")

	// --- daemon maps ---
	mapsCmd := &cobra.Command{
		Use:   "maps",
		Short: "List identity mappings",
		RunE: func(cmd *cobra.Command, args []string) error {
			if configPath == "" {
				configPath = daemon.DefaultConfigPath()
			}
			cfg, err := daemon.ReadConfig(configPath)
			if err != nil {
				return fmt.Errorf("no config at %s: %w", configPath, err)
			}
			if len(cfg.Mappings) == 0 {
				fmt.Println("No mappings configured.")
				return nil
			}
			fmt.Printf("%-30s %-15s %s\n", "IDENTITY", "LOCAL USER", "SHELL")
			for _, m := range cfg.Mappings {
				fmt.Printf("%-30s %-15s %s\n", m.Identity, m.LocalUser, m.Shell)
			}
			return nil
		},
	}

	// --- daemon set-key ---
	setKeyCmd := &cobra.Command{
		Use:   "set-key [key]",
		Short: "Set the API key for daemon authentication",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if configPath == "" {
				configPath = daemon.DefaultConfigPath()
			}
			cfg, err := daemon.ReadConfig(configPath)
			if err != nil {
				return fmt.Errorf("no config at %s — run 'phosphor daemon map' first: %w", configPath, err)
			}
			cfg.ApiKey = args[0]
			if err := daemon.WriteConfig(configPath, cfg); err != nil {
				return err
			}
			fmt.Println("API key set successfully.")
			return nil
		},
	}

	daemonCmd.PersistentFlags().StringVar(&configPath, "config", "", "Config file path (default: platform-specific)")
	daemonCmd.AddCommand(runCmd, installCmd, uninstallCmd, mapCmd, unmapCmd, mapsCmd, setKeyCmd)
	return daemonCmd
}
