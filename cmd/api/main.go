// Command api is the entrypoint for the WalletCore HTTP service.
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

	"github.com/joho/godotenv"

	"walletcore/internal/config"
	"walletcore/internal/database"
	"walletcore/internal/server"
)

func main() {
	// Structured JSON logging to stdout.
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	// Load .env if present. Missing file is fine (e.g. in containers).
	_ = godotenv.Load()

	cfg := config.Load()
	ctx := context.Background()

	// Connect to Postgres (fails fast if unreachable).
	pool, err := database.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	slog.Info("connected to database")

	srv := &http.Server{
		Addr:         ":" + cfg.HTTPPort,
		Handler:      server.New(cfg, pool).Router(),
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	}

	// Start the server in a goroutine so we can wait for shutdown signals.
	go func() {
		slog.Info("http server starting", "port", cfg.HTTPPort, "env", cfg.AppEnv)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("http server error", "error", err)
			os.Exit(1)
		}
	}()

	// Block until we receive an interrupt or terminate signal.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	// Graceful shutdown: stop accepting new connections, finish in-flight ones.
	slog.Info("shutting down...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("graceful shutdown failed", "error", err)
	}
	slog.Info("stopped")
}
