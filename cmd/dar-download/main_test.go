package main

import (
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"
)

func TestHTTPServerHasExplicitResourceBounds(t *testing.T) {
	t.Parallel()

	server := newHTTPServer(
		8000,
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if server.Addr != ":8000" {
		t.Fatalf("Addr = %q", server.Addr)
	}
	if server.ReadHeaderTimeout != 5*time.Second ||
		server.ReadTimeout != 15*time.Second ||
		server.WriteTimeout != 10*time.Minute ||
		server.IdleTimeout != 60*time.Second ||
		server.MaxHeaderBytes != 32*1024 {
		t.Fatalf("server limits = %#v", server)
	}
	if server.ErrorLog == nil {
		t.Fatal("ErrorLog must be explicitly configured")
	}
}
