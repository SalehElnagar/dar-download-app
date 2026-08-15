package testdata

import (
	"log/slog"
	"net/http"
)

func exerciseHeaderLoggingRule(logger *slog.Logger, request *http.Request) {
	// ruleid: dar-no-sensitive-header-logging
	logger.Info("request received", "headers", request.Header)
	// ruleid: dar-no-sensitive-header-logging
	logger.Warn("identity received", "principal", request.Header.Get("X-MS-CLIENT-PRINCIPAL"))

	// ok: dar-no-sensitive-header-logging
	_ = authenticate(request.Header)
}

func authenticate(_ http.Header) bool {
	return true
}
