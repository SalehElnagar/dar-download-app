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

	"github.com/SalehElnagar/dar-download-app/internal/blob"
	"github.com/SalehElnagar/dar-download-app/internal/config"
	"github.com/SalehElnagar/dar-download-app/internal/httpapi"
)

var (
	version  = "dev"
	revision = "unknown"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, logger); err != nil {
		logger.Error("service stopped", "event", "fatal")
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger) error {
	cfg, err := config.LoadEnvironment()
	if err != nil {
		return errors.New("configuration rejected")
	}
	store, err := blob.NewAzureStore(cfg)
	if err != nil {
		return errors.New("storage initialization rejected")
	}
	handler := httpapi.New(cfg, store, logger)
	server := newHTTPServer(cfg.Port, handler, logger)
	server.BaseContext = func(net.Listener) context.Context { return ctx }

	logger.Info(
		"service starting",
		"event", "startup",
		"port", cfg.Port,
		"version", version,
		"revision", revision,
	)
	serveErrors := make(chan error, 1)
	go func() {
		serveErrors <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if shutdownErr := server.Shutdown(shutdownCtx); shutdownErr != nil {
			return errors.New("graceful shutdown failed")
		}
		serveErr := <-serveErrors
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			return errors.New("HTTP server failed")
		}
		logger.Info("service stopped", "event", "shutdown")
		return nil
	case serveErr := <-serveErrors:
		if serveErr == nil || errors.Is(serveErr, http.ErrServerClosed) {
			return nil
		}
		return errors.New("HTTP server failed")
	}
}

func newHTTPServer(port int, handler http.Handler, _ *slog.Logger) *http.Server {
	return &http.Server{
		Addr:              ":" + strconv.Itoa(port),
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      10 * time.Minute,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    32 * 1024,
		ErrorLog:          log.New(io.Discard, "", 0),
	}
}
