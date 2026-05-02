package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"nanobot-go/internal/app"
	"nanobot-go/internal/config"

	"github.com/spf13/cobra"
)

var gatewayCmd = &cobra.Command{
	Use:   "gateway",
	Short: "Start the nanobot gateway server",
	Run:   runGateway,
}

var (
	gatewayPortFlag      int
	gatewayWorkspaceFlag string
	gatewayVerboseFlag   bool
	gatewayConfigFlag    string
)

func init() {
	gatewayCmd.Flags().IntVarP(&gatewayPortFlag, "port", "p", 0, "Gateway port")
	gatewayCmd.Flags().StringVarP(&gatewayWorkspaceFlag, "workspace", "w", "", "Workspace directory")
	gatewayCmd.Flags().BoolVarP(&gatewayVerboseFlag, "verbose", "v", false, "Verbose output")
	gatewayCmd.Flags().StringVarP(&gatewayConfigFlag, "config", "c", "", "Config file path")
}

func runGateway(cmd *cobra.Command, args []string) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set log level
	if gatewayVerboseFlag {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		})))
	}

	cfg, err := config.Load(gatewayConfigFlag)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Override port if specified
	if gatewayPortFlag > 0 {
		cfg.Gateway.Port = gatewayPortFlag
	}

	// Override workspace if specified
	if gatewayWorkspaceFlag != "" {
		cfg.Agents.Workspace = gatewayWorkspaceFlag
	}

	// Create application
	application, err := app.New(cfg)
	if err != nil {
		slog.Error("failed to create application", "error", err)
		os.Exit(1)
	}

	// Handle shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigCh
		slog.Info("received signal, shutting down", "signal", sig)
		application.Stop()
		cancel()
	}()

	// Start application
	gatewayAddr := fmt.Sprintf("%s:%d", cfg.Gateway.Host, cfg.Gateway.Port)
	slog.Info("gateway starting", "address", gatewayAddr)
	fmt.Printf("%s Starting nanobot gateway v%s on port %d...\n", logo, version, cfg.Gateway.Port)

	if err := application.Start(ctx); err != nil {
		slog.Error("failed to start application", "error", err)
		os.Exit(1)
	}

	<-ctx.Done()
	slog.Info("gateway stopped")
}
