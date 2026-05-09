package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"ori/internal/app"
	"ori/internal/config"
	"ori/internal/monitoring"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))

	cfg, err := config.Load("")
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	application, err := app.New(cfg)
	if err != nil {
		slog.Error("failed to create application", "error", err)
		os.Exit(1)
	}

	// Start the application first so a Start failure doesn't leave a
	// health server running behind it.
	gatewayAddr := fmt.Sprintf("%s:%d", cfg.Gateway.Host, cfg.Gateway.Port)
	slog.Info("gateway starting", "address", gatewayAddr)
	if err := application.Start(application.Context()); err != nil {
		slog.Error("failed to start application", "error", err)
		os.Exit(1)
	}

	healthAddr := fmt.Sprintf("%s:%d", cfg.Gateway.Host, cfg.Gateway.Port+1)
	healthSrv := monitoring.StartHealthServer(healthAddr)
	slog.Info("health/metrics server started", "address", healthAddr)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		slog.Info("received signal, shutting down", "signal", sig)
		_ = healthSrv.Close()
		application.Stop()
	}()

	<-application.Done()
	slog.Info("gateway stopped")
}
