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
	_, authenticated := auth.Authenticate(
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
	handler.stream(writer, request, selectedDownload{
		target:    target,
		snapshot:  snapshot,
		selection: selection,
	})
}

type downloadTarget struct {
	blobName string
	fileName string
}

type selectedDownload struct {
	target    downloadTarget
	snapshot  blob.Snapshot
	selection download.Selection
}

func (handler *Handler) stream(
	writer http.ResponseWriter,
	request *http.Request,
	selected selectedDownload,
) {
	selection := selected.selection
	if selection.Length == 0 {
		setDownloadHeaders(writer.Header(), selected)
		writer.WriteHeader(http.StatusOK)
		return
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
		return
	}

	setDownloadHeaders(writer.Header(), selected)
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
			request.Context(), selected.target.blobName, offset, length, selected.snapshot.ETag,
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
	}, true
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
