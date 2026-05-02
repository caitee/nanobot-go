package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"nanobot-go/internal/app"
	"nanobot-go/internal/config"
	"nanobot-go/internal/monitoring"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))

	cfg, err := config.Load("")
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Create application
	application, err := app.New(cfg)
	if err != nil {
		slog.Error("failed to create application", "error", err)
		os.Exit(1)
	}

	// Start health and metrics server
	healthAddr := fmt.Sprintf("%s:%d", cfg.Gateway.Host, cfg.Gateway.Port+1)
	healthSrv := monitoring.StartHealthServer(healthAddr)
	slog.Info("health/metrics server starting", "address", healthAddr)

	// Handle shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		slog.Info("received signal, shutting down", "signal", sig)
		application.Stop()
		healthSrv.Close()
	}()

	// Start application (initializes plugins, starts agent loop, channels, cron)
	gatewayAddr := fmt.Sprintf("%s:%d", cfg.Gateway.Host, cfg.Gateway.Port)
	slog.Info("gateway starting", "address", gatewayAddr)

	if err := application.Start(application.Context()); err != nil {
		slog.Error("failed to start application", "error", err)
		os.Exit(1)
	}

	<-application.Done()
	slog.Info("gateway stopped")
}
