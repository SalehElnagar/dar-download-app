package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/SalehElnagar/dar-download-app/internal/distribution"
)

var (
	version  = "dev"
	revision = "unknown"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	config, err := distribution.LoadWorkerEnvironment()
	if err != nil {
		logger.Error("worker configuration rejected", "event", "fatal")
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	logger.Info("worker candidate", "event", "candidate", "version", version, "revision", revision)
	if err := distribution.RunWorker(ctx, logger, config); err != nil {
		logger.Error("worker stopped", "event", "fatal")
		os.Exit(1)
	}
}
