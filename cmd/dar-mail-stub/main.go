package main

import (
	"context"
	"errors"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/SalehElnagar/dar-download-app/internal/distribution"
)

var (
	version  = "dev"
	revision = "unknown"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	port, err := boundedEnvInt("HARMONY_DAR_STUB_PORT", 8080, 1024, 65535)
	if err != nil {
		logger.Error("stub configuration rejected", "event", "fatal")
		os.Exit(1)
	}
	timeoutSeconds, err := boundedEnvInt("HARMONY_DAR_STUB_TIMEOUT_SECONDS", 15, 1, 30)
	if err != nil {
		logger.Error("stub configuration rejected", "event", "fatal")
		os.Exit(1)
	}
	stub, err := distribution.NewMailStub(
		environmentOrDefault("HARMONY_DAR_STUB_SCENARIO", "accepted"),
		time.Duration(timeoutSeconds)*time.Second,
	)
	if err != nil {
		logger.Error("stub configuration rejected", "event", "fatal")
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	server := &http.Server{
		Addr: ":" + strconv.Itoa(port), Handler: stub,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second,
		WriteTimeout: 30 * time.Second, IdleTimeout: 30 * time.Second,
		MaxHeaderBytes: 16 * 1024, ErrorLog: log.New(io.Discard, "", 0),
		BaseContext: func(net.Listener) context.Context { return ctx },
	}
	serveErrors := make(chan error, 1)
	go func() { serveErrors <- server.ListenAndServe() }()
	logger.Info("mail stub starting", "event", "startup", "version", version, "revision", revision)
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error("stub shutdown failed", "event", "fatal")
			os.Exit(1)
		}
		if err := <-serveErrors; err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("stub stopped", "event", "fatal")
			os.Exit(1)
		}
	case err := <-serveErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("stub stopped", "event", "fatal")
			os.Exit(1)
		}
	}
}

func boundedEnvInt(name string, defaultValue, minimum, maximum int) (int, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return defaultValue, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < minimum || value > maximum {
		return 0, errors.New("invalid integer environment value")
	}
	return value, nil
}

func environmentOrDefault(name, defaultValue string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return defaultValue
}
