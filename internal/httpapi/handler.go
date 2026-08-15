// Package httpapi exposes the complete DAR download HTTP surface.
package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/SalehElnagar/dar-download-app/internal/auth"
	"github.com/SalehElnagar/dar-download-app/internal/blob"
	"github.com/SalehElnagar/dar-download-app/internal/config"
	"github.com/SalehElnagar/dar-download-app/internal/download"
)

const (
	downloadPrefix = "/v1/releases/"
	downloadSuffix = "/download"
)

// Handler authenticates, authorizes, selects, and streams exact release objects.
type Handler struct {
	config config.Config
	store  blob.Store
	logger *slog.Logger
}

// New constructs the application handler over one validated policy and narrow store.
func New(cfg config.Config, store blob.Store, logger *slog.Logger) http.Handler {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Handler{config: cfg, store: store, logger: logger}
}

// ServeHTTP implements the anonymous health route and protected download route.
func (handler *Handler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	setSecurityHeaders(writer.Header())
	if request.Method != http.MethodGet {
		writer.Header().Set("Allow", http.MethodGet)
		writeJSON(writer, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	if request.URL.Path == "/healthz" {
		writeHealth(writer)
		return
	}

	releaseID, matches := releaseIDFromPath(request.URL.Path)
	if !matches {
		writeJSON(writer, http.StatusNotFound, "release_not_found")
		return
	}
	identity, authenticated := auth.Authenticate(
		request.Header,
		handler.config.OIDCIssuer,
		handler.config.TrustedIdentityMode,
		handler.config.AzureContainerAppsTenantID,
	)
	if !authenticated {
		writeJSON(writer, http.StatusUnauthorized, "authentication_required")
		return
	}
	release, found := handler.config.Release(releaseID)
	if !found {
		writeJSON(writer, http.StatusNotFound, "release_not_found")
		return
	}
	if !release.Allows(identity.Subject) {
		writeJSON(writer, http.StatusForbidden, "authorization_denied")
		return
	}

	snapshot, err := handler.store.Stat(request.Context(), release.BlobName)
	if err != nil {
		handler.writeStorageError(writer, request.Context(), err)
		return
	}
	if snapshot.Size > config.MaxObjectSize {
		writeJSON(writer, http.StatusRequestEntityTooLarge, "release_too_large")
		return
	}
	selection, err := download.Select(
		request.Header.Values("Range"),
		request.Header.Values("If-Range"),
		snapshot.Size,
		snapshot.ETag,
	)
	if err != nil {
		writer.Header().Set("Accept-Ranges", "bytes")
		writer.Header().Set("Content-Range", "bytes */"+strconv.FormatInt(snapshot.Size, 10))
		writeJSON(writer, http.StatusRequestedRangeNotSatisfiable, "invalid_range")
		return
	}
	handler.stream(writer, request, release, snapshot, selection)
}

func (handler *Handler) stream(
	writer http.ResponseWriter,
	request *http.Request,
	release config.ReleasePolicy,
	snapshot blob.Snapshot,
	selection download.Selection,
) {
	if selection.Length == 0 {
		setDownloadHeaders(writer.Header(), release, snapshot, selection)
		writer.WriteHeader(http.StatusOK)
		return
	}

	firstLength := min(selection.Length, config.MaxStorageSegment)
	reader, err := handler.store.OpenRange(
		request.Context(),
		release.BlobName,
		selection.Offset,
		firstLength,
		snapshot.ETag,
	)
	if err != nil {
		handler.writeStorageError(writer, request.Context(), err)
		return
	}

	setDownloadHeaders(writer.Header(), release, snapshot, selection)
	status := http.StatusOK
	if selection.Partial {
		status = http.StatusPartialContent
	}
	writer.WriteHeader(status)
	if !handler.copySegment(writer, reader, firstLength) {
		return
	}

	offset := selection.Offset + firstLength
	remaining := selection.Length - firstLength
	for remaining > 0 {
		if request.Context().Err() != nil {
			return
		}
		length := min(remaining, config.MaxStorageSegment)
		reader, openErr := handler.store.OpenRange(
			request.Context(), release.BlobName, offset, length, snapshot.ETag,
		)
		if openErr != nil {
			handler.logStreamFailure()
			return
		}
		if !handler.copySegment(writer, reader, length) {
			return
		}
		offset += length
		remaining -= length
	}
}

func (handler *Handler) copySegment(
	writer http.ResponseWriter,
	reader io.ReadCloser,
	length int64,
) bool {
	_, copyErr := io.CopyN(writer, reader, length)
	closeErr := reader.Close()
	if copyErr != nil || closeErr != nil {
		handler.logStreamFailure()
		return false
	}
	return true
}

func (handler *Handler) writeStorageError(
	writer http.ResponseWriter,
	ctx context.Context,
	err error,
) {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
		return
	}
	if errors.Is(err, blob.ErrNotFound) {
		writeJSON(writer, http.StatusNotFound, "release_not_found")
		return
	}
	writeJSON(writer, http.StatusBadGateway, "storage_unavailable")
}

func (handler *Handler) logStreamFailure() {
	handler.logger.Warn("download stream terminated", "event", "stream_failure")
}

func releaseIDFromPath(path string) (string, bool) {
	if !strings.HasPrefix(path, downloadPrefix) || !strings.HasSuffix(path, downloadSuffix) {
		return "", false
	}
	releaseID := strings.TrimSuffix(strings.TrimPrefix(path, downloadPrefix), downloadSuffix)
	if !config.IsReleaseID(releaseID) {
		return "", false
	}
	return releaseID, true
}

func setDownloadHeaders(
	headers http.Header,
	release config.ReleasePolicy,
	snapshot blob.Snapshot,
	selection download.Selection,
) {
	headers.Set("Accept-Ranges", "bytes")
	headers.Set("Cache-Control", "private, no-store")
	headers.Set("Content-Disposition", `attachment; filename="`+release.DownloadName+`"`)
	headers.Set("Content-Length", strconv.FormatInt(selection.Length, 10))
	headers.Set("Content-Type", "application/octet-stream")
	headers.Set("ETag", snapshot.ETag)
	if selection.Partial && selection.Range != nil {
		headers.Set(
			"Content-Range",
			"bytes "+strconv.FormatInt(selection.Range.Start, 10)+"-"+
				strconv.FormatInt(selection.Range.End, 10)+"/"+
				strconv.FormatInt(snapshot.Size, 10),
		)
	}
}

func setSecurityHeaders(headers http.Header) {
	headers.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; sandbox")
	headers.Set("Cross-Origin-Resource-Policy", "same-origin")
	headers.Set("Permissions-Policy", "camera=(), geolocation=(), microphone=()")
	headers.Set("Referrer-Policy", "no-referrer")
	headers.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
	headers.Set("X-Content-Type-Options", "nosniff")
	headers.Set("X-Frame-Options", "DENY")
}

func writeHealth(writer http.ResponseWriter) {
	const body = `{"service":"dar-download","status":"ok"}`
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Content-Length", strconv.Itoa(len(body)))
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(writer, body)
}

func writeJSON(writer http.ResponseWriter, status int, code string) {
	body := `{"error":"` + code + `"}`
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Content-Length", strconv.Itoa(len(body)))
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_, _ = io.WriteString(writer, body)
}
