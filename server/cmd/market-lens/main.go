package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"market-lens/server/internal/api"
	"market-lens/server/internal/config"
	"market-lens/server/internal/db"
)

var version = "dev"

func main() {
	if err := run(); err != nil {
		slog.Error("market lens stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	configureLogging(cfg.IsProduction())
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool); err != nil {
		return err
	}

	handler := api.NewRouter(api.Dependencies{
		Database: pool, AllowedOrigins: cfg.AllowedOrigins, StaticDir: cfg.StaticDir, Version: version,
	})
	server := &http.Server{
		Addr: ":" + cfg.Port, Handler: handler,
		ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 120 * time.Second,
	}
	serverErr := make(chan error, 1)
	go func() {
		slog.Info("market lens starting", "address", server.Addr, "environment", cfg.Environment, "version", version)
		serverErr <- server.ListenAndServe()
	}()

	select {
	case err := <-serverErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		slog.Info("shutting down")
		return server.Shutdown(shutdownCtx)
	}
}

func configureLogging(production bool) {
	options := &slog.HandlerOptions{Level: slog.LevelInfo}
	if production {
		slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, options)))
	} else {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, options)))
	}
}
