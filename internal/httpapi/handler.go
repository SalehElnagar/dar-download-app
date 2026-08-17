// Package httpapi exposes the complete DAR download HTTP surface.
package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/SalehElnagar/dar-download-app/internal/auth"
	"github.com/SalehElnagar/dar-download-app/internal/blob"
	"github.com/SalehElnagar/dar-download-app/internal/config"
	"github.com/SalehElnagar/dar-download-app/internal/download"
)

const (
	maxVersionBytes  = 96
	maxFileNameBytes = 128
)

// Handler authenticates callers, selects exact path-derived objects, and streams them.
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

	target, matches := downloadTargetFromRequest(request)
	if !matches {
		writeJSON(writer, http.StatusNotFound, "release_not_found")
		return
	}
	identity, authenticated := auth.Authenticate(
		request.Header,
		auth.BoundaryPolicy{
			ExpectedIssuer:           handler.config.OIDCIssuer,
			Mode:                     handler.config.TrustedIdentityMode,
			ExpectedAzureTenantID:    handler.config.AzureContainerAppsTenantID,
			ExpectedOIDCProviderName: handler.config.OIDCProviderName,
		},
	)
	if !authenticated {
		writeJSON(writer, http.StatusUnauthorized, "authentication_required")
		return
	}
	sessionID, err := newSessionID()
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, "audit_unavailable")
		return
	}
	snapshot, err := handler.store.Stat(request.Context(), target.blobName)
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
	selected := selectedDownload{
		target:    target,
		snapshot:  snapshot,
		selection: selection,
	}
	handler.logger.Info(
		"download started",
		"schema_version", "1.0",
		"event", "dar.download.started",
		"occurred_at", time.Now().UTC().Format(time.RFC3339Nano),
		"session_id", sessionID,
		"subject", identity.Subject,
		"version", target.version,
		"file_name", target.fileName,
		"requested_bytes", selection.Length,
	)
	streamed := handler.stream(writer, request, selected)
	handler.logger.Info(
		"download terminal",
		"schema_version", "1.0",
		"event", "dar.download."+strings.ToLower(streamed.outcome),
		"occurred_at", time.Now().UTC().Format(time.RFC3339Nano),
		"session_id", sessionID,
		"subject", identity.Subject,
		"version", target.version,
		"file_name", target.fileName,
		"streamed_bytes", streamed.bytes,
		"outcome", streamed.outcome,
	)
}

type downloadTarget struct {
	blobName string
	fileName string
	version  string
}

type selectedDownload struct {
	target    downloadTarget
	snapshot  blob.Snapshot
	selection download.Selection
}

type streamResult struct {
	outcome string
	bytes   int64
}

func (handler *Handler) stream(
	writer http.ResponseWriter,
	request *http.Request,
	selected selectedDownload,
) streamResult {
	selection := selected.selection
	if selection.Length == 0 {
		setDownloadHeaders(writer.Header(), selected)
		writer.WriteHeader(http.StatusOK)
		return streamResult{outcome: "STREAM_COMPLETED"}
	}

	firstLength := min(selection.Length, config.MaxStorageSegment)
	reader, err := handler.store.OpenRange(
		request.Context(),
		selected.target.blobName,
		selection.Offset,
		firstLength,
		selected.snapshot.ETag,
	)
	if err != nil {
		handler.writeStorageError(writer, request.Context(), err)
		return streamResult{outcome: "STORAGE_UNAVAILABLE"}
	}

	setDownloadHeaders(writer.Header(), selected)
	status := http.StatusOK
	if selection.Partial {
		status = http.StatusPartialContent
	}
	writer.WriteHeader(status)
	written, copied := handler.copySegment(writer, reader, firstLength)
	totalWritten := written
	if !copied {
		return streamResult{outcome: streamFailureOutcome(request.Context()), bytes: totalWritten}
	}

	offset := selection.Offset + firstLength
	remaining := selection.Length - firstLength
	for remaining > 0 {
		if request.Context().Err() != nil {
			return streamResult{outcome: "STREAM_ABANDONED", bytes: totalWritten}
		}
		length := min(remaining, config.MaxStorageSegment)
		reader, openErr := handler.store.OpenRange(
			request.Context(), selected.target.blobName, offset, length, selected.snapshot.ETag,
		)
		if openErr != nil {
			handler.logStreamFailure()
			return streamResult{outcome: "STREAM_FAILED", bytes: totalWritten}
		}
		written, copied = handler.copySegment(writer, reader, length)
		totalWritten += written
		if !copied {
			return streamResult{outcome: streamFailureOutcome(request.Context()), bytes: totalWritten}
		}
		offset += length
		remaining -= length
	}
	return streamResult{outcome: "STREAM_COMPLETED", bytes: totalWritten}
}

func (handler *Handler) copySegment(
	writer http.ResponseWriter,
	reader io.ReadCloser,
	length int64,
) (int64, bool) {
	written, copyErr := io.CopyN(writer, reader, length)
	closeErr := reader.Close()
	if copyErr != nil || closeErr != nil {
		handler.logStreamFailure()
		return written, false
	}
	return written, true
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

func downloadTargetFromRequest(request *http.Request) (downloadTarget, bool) {
	if request.URL == nil || request.URL.Opaque != "" || request.URL.RawPath != "" ||
		request.URL.RawQuery != "" || request.URL.ForceQuery ||
		strings.Contains(request.RequestURI, "%") {
		return downloadTarget{}, false
	}
	parts := strings.Split(request.URL.Path, "/")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "v1" ||
		parts[2] != "releases" || parts[4] != "download" ||
		!validDownloadSegment(parts[3], maxVersionBytes) ||
		!validDownloadSegment(parts[5], maxFileNameBytes) {
		return downloadTarget{}, false
	}
	return downloadTarget{
		blobName: parts[3] + "/" + parts[5],
		fileName: parts[5],
		version:  parts[3],
	}, true
}

func newSessionID() (string, error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return hex.EncodeToString(random), nil
}

func streamFailureOutcome(ctx context.Context) string {
	if ctx.Err() != nil {
		return "STREAM_ABANDONED"
	}
	return "STREAM_FAILED"
}

func validDownloadSegment(value string, maximumBytes int) bool {
	if value == "" || len(value) > maximumBytes || strings.Trim(value, ".") == "" {
		return false
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		if (character < 'A' || character > 'Z') &&
			(character < 'a' || character > 'z') &&
			(character < '0' || character > '9') &&
			character != '.' && character != '_' && character != '-' {
			return false
		}
	}
	return true
}

func setDownloadHeaders(
	headers http.Header,
	selected selectedDownload,
) {
	selection := selected.selection
	headers.Set("Accept-Ranges", "bytes")
	headers.Set("Cache-Control", "private, no-store")
	headers.Set("Content-Disposition", `attachment; filename="`+selected.target.fileName+`"`)
	headers.Set("Content-Length", strconv.FormatInt(selection.Length, 10))
	headers.Set("Content-Type", "application/octet-stream")
	headers.Set("ETag", selected.snapshot.ETag)
	if selection.Partial && selection.Range != nil {
		headers.Set(
			"Content-Range",
			"bytes "+strconv.FormatInt(selection.Range.Start, 10)+"-"+
				strconv.FormatInt(selection.Range.End, 10)+"/"+
				strconv.FormatInt(selected.snapshot.Size, 10),
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
	// nosemgrep: go.lang.security.audit.xss.no-io-writestring-to-responsewriter.no-io-writestring-to-responsewriter -- compile-time JSON with an application/json content type
	_, _ = io.WriteString(writer, body)
}

func writeJSON(writer http.ResponseWriter, status int, code string) {
	body := `{"error":"` + code + `"}`
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Content-Length", strconv.Itoa(len(body)))
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	// nosemgrep: go.lang.security.audit.xss.no-io-writestring-to-responsewriter.no-io-writestring-to-responsewriter -- code is an internal bounded reason token, not HTML or user input
	_, _ = io.WriteString(writer, body)
}
