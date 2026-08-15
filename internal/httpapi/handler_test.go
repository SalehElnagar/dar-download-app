package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/SalehElnagar/dar-download-app/internal/blob"
	"github.com/SalehElnagar/dar-download-app/internal/config"
	"github.com/SalehElnagar/dar-download-app/internal/httpapi"
	"github.com/SalehElnagar/dar-download-app/internal/testsupport"
)

const (
	oidcIssuer        = "https://identity.example.com/realms/customers/"
	managedIdentityID = "22222222-2222-4222-8222-222222222222"
	allowedSubject    = "customer:Case-Sensitive-001"
	releaseID         = "dar_01JABCDEF0123456789XYZ"
	version           = "v26.8.31.01"
	fileName          = "example.dar"
	blobName          = version + "/" + fileName
	downloadPath      = "/v1/releases/" + version + "/download/" + fileName
)

func testConfig(t *testing.T) config.Config {
	t.Helper()
	cfg, err := config.ParseEnvironment(map[string]string{
		config.OIDCIssuerEnv:              oidcIssuer,
		config.StorageAccountNameEnv:      "stdardownloadpoc01",
		config.StorageContainerEnv:        "dar-releases",
		config.ManagedIdentityClientIDEnv: managedIdentityID,
	})
	if err != nil {
		t.Fatalf("config.ParseEnvironment() error = %v", err)
	}
	return cfg
}

func newHandler(t *testing.T, storage blob.Store) http.Handler {
	t.Helper()
	return httpapi.New(testConfig(t), storage, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func request(t *testing.T, handler http.Handler, method, path string, headers http.Header) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	for name, values := range headers {
		for _, value := range values {
			req.Header.Add(name, value)
		}
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

func authenticatedHeaders() http.Header {
	return testsupport.OIDCHeaders(oidcIssuer, allowedSubject)
}

func assertJSONError(t *testing.T, recorder *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if recorder.Code != status {
		t.Fatalf("status = %d, want %d; body = %q", recorder.Code, status, recorder.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if len(body) != 1 || body["error"] != code {
		t.Fatalf("body = %#v", body)
	}
}

func TestHealthIsAnonymousAndNeverReadsStorage(t *testing.T) {
	t.Parallel()

	storage := &testsupport.Storage{Objects: map[string]testsupport.Object{}}
	recorder := request(t, newHandler(t, storage), http.MethodGet, "/healthz", nil)
	if recorder.Code != http.StatusOK || recorder.Body.String() != `{"service":"dar-download","status":"ok"}` {
		t.Fatalf("response = %d %q", recorder.Code, recorder.Body.String())
	}
	statCalls, openCalls, _ := storage.Counts()
	if len(statCalls) != 0 || len(openCalls) != 0 {
		t.Fatalf("storage calls = %#v %#v", statCalls, openCalls)
	}
	for name, value := range map[string]string{
		"Cache-Control":                "no-store",
		"Content-Type":                 "application/json",
		"Cross-Origin-Resource-Policy": "same-origin",
		"X-Content-Type-Options":       "nosniff",
		"X-Frame-Options":              "DENY",
		"Referrer-Policy":              "no-referrer",
	} {
		if recorder.Header().Get(name) != value {
			t.Errorf("%s = %q, want %q", name, recorder.Header().Get(name), value)
		}
	}
}

func TestFullDownloadStreamsExactSegmentsAndHeaders(t *testing.T) {
	t.Parallel()

	payload := bytes.Repeat([]byte("x"), int(config.MaxStorageSegment+17))
	storage := &testsupport.Storage{Objects: map[string]testsupport.Object{
		blobName: {Data: payload, ETag: `"etag-v1"`},
	}}
	recorder := request(
		t,
		newHandler(t, storage),
		http.MethodGet,
		downloadPath,
		authenticatedHeaders(),
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %q", recorder.Code, recorder.Body.String())
	}
	if !bytes.Equal(recorder.Body.Bytes(), payload) {
		t.Fatalf("downloaded %d bytes, want %d", recorder.Body.Len(), len(payload))
	}
	for name, value := range map[string]string{
		"Accept-Ranges":          "bytes",
		"Cache-Control":          "private, no-store",
		"Content-Disposition":    `attachment; filename="example.dar"`,
		"Content-Length":         strconv.Itoa(len(payload)),
		"Content-Type":           "application/octet-stream",
		"ETag":                   `"etag-v1"`,
		"X-Content-Type-Options": "nosniff",
	} {
		if recorder.Header().Get(name) != value {
			t.Errorf("%s = %q, want %q", name, recorder.Header().Get(name), value)
		}
	}
	_, openCalls, maxActive := storage.Counts()
	if len(openCalls) != 2 || openCalls[0].Length != config.MaxStorageSegment || openCalls[1].Length != 17 {
		t.Fatalf("open calls = %#v", openCalls)
	}
	if maxActive != 1 {
		t.Fatalf("max active readers = %d, want 1", maxActive)
	}
}

func TestPartialDownloadReturnsExactSelectedBytes(t *testing.T) {
	t.Parallel()

	storage := &testsupport.Storage{Objects: map[string]testsupport.Object{
		blobName: {Data: []byte("0123456789"), ETag: `"etag-v1"`},
	}}
	headers := authenticatedHeaders()
	headers.Set("Range", "bytes=2-5")
	headers.Set("If-Range", `"etag-v1"`)
	recorder := request(
		t,
		newHandler(t, storage),
		http.MethodGet,
		downloadPath,
		headers,
	)
	if recorder.Code != http.StatusPartialContent || recorder.Body.String() != "2345" {
		t.Fatalf("response = %d %q", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("Content-Range") != "bytes 2-5/10" ||
		recorder.Header().Get("Content-Length") != "4" {
		t.Fatalf("headers = %#v", recorder.Header())
	}
}

func TestStaleIfRangeReturnsFullCurrentRepresentation(t *testing.T) {
	t.Parallel()

	storage := &testsupport.Storage{Objects: map[string]testsupport.Object{
		blobName: {Data: []byte("0123456789"), ETag: `"etag-v1"`},
	}}
	headers := authenticatedHeaders()
	headers.Set("Range", "bytes=2-5")
	headers.Set("If-Range", `W/"etag-v1"`)
	recorder := request(
		t,
		newHandler(t, storage),
		http.MethodGet,
		downloadPath,
		headers,
	)
	if recorder.Code != http.StatusOK || recorder.Body.String() != "0123456789" {
		t.Fatalf("response = %d %q", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("Content-Range") != "" {
		t.Fatalf("Content-Range = %q", recorder.Header().Get("Content-Range"))
	}
}

func TestInvalidRangeReturns416WithoutOpeningBody(t *testing.T) {
	t.Parallel()

	storage := &testsupport.Storage{Objects: map[string]testsupport.Object{
		blobName: {Data: []byte("0123456789"), ETag: `"etag-v1"`},
	}}
	headers := authenticatedHeaders()
	headers.Set("Range", "bytes=10-11")
	recorder := request(
		t,
		newHandler(t, storage),
		http.MethodGet,
		downloadPath,
		headers,
	)
	assertJSONError(t, recorder, http.StatusRequestedRangeNotSatisfiable, "invalid_range")
	if recorder.Header().Get("Content-Range") != "bytes */10" {
		t.Fatalf("Content-Range = %q", recorder.Header().Get("Content-Range"))
	}
	_, openCalls, _ := storage.Counts()
	if len(openCalls) != 0 {
		t.Fatalf("open calls = %#v", openCalls)
	}
}

func TestZeroLengthDownloadDoesNotOpenBody(t *testing.T) {
	t.Parallel()

	storage := &testsupport.Storage{Objects: map[string]testsupport.Object{
		blobName: {Data: []byte{}, ETag: `"etag-v1"`},
	}}
	recorder := request(
		t,
		newHandler(t, storage),
		http.MethodGet,
		downloadPath,
		authenticatedHeaders(),
	)
	if recorder.Code != http.StatusOK || recorder.Body.Len() != 0 ||
		recorder.Header().Get("Content-Length") != "0" {
		t.Fatalf("response = %d %q, headers = %#v", recorder.Code, recorder.Body.String(), recorder.Header())
	}
	_, openCalls, _ := storage.Counts()
	if len(openCalls) != 0 {
		t.Fatalf("open calls = %#v", openCalls)
	}
}

func TestAuthenticationAndInvalidRoutesAreDeniedBeforeStorage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		path    string
		headers http.Header
		status  int
		code    string
	}{
		{name: "missing identity", path: downloadPath, status: 401, code: "authentication_required"},
		{
			name: "old static route", path: "/v1/releases/" + releaseID + "/download",
			headers: authenticatedHeaders(), status: 404, code: "release_not_found",
		},
		{
			name: "path-like version", path: "/v1/releases/../download/example.dar",
			headers: authenticatedHeaders(), status: 404, code: "release_not_found",
		},
	}
	for _, testCase := range tests {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			storage := &testsupport.Storage{Objects: map[string]testsupport.Object{}}
			recorder := request(
				t,
				newHandler(t, storage),
				http.MethodGet,
				testCase.path,
				testCase.headers,
			)
			assertJSONError(t, recorder, testCase.status, testCase.code)
			statCalls, openCalls, _ := storage.Counts()
			if len(statCalls) != 0 || len(openCalls) != 0 {
				t.Fatalf("storage calls = %#v %#v", statCalls, openCalls)
			}
		})
	}
}

func TestStorageErrorsAreBoundedBeforeBodyCommit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		statErr error
		openErr error
		status  int
		code    string
	}{
		{name: "missing", statErr: blob.ErrNotFound, status: 404, code: "release_not_found"},
		{name: "stat unavailable", statErr: blob.ErrUnavailable, status: 502, code: "storage_unavailable"},
		{name: "changed on first open", openErr: blob.ErrChanged, status: 502, code: "storage_unavailable"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			storage := &testsupport.Storage{
				Objects: map[string]testsupport.Object{blobName: {Data: []byte("abc"), ETag: `"etag-v1"`}},
				StatErr: tt.statErr,
				OpenErr: tt.openErr,
			}
			recorder := request(
				t,
				newHandler(t, storage),
				http.MethodGet,
				downloadPath,
				authenticatedHeaders(),
			)
			assertJSONError(t, recorder, tt.status, tt.code)
		})
	}
}

func TestMethodAndUnknownRouteAreBounded(t *testing.T) {
	t.Parallel()

	storage := &testsupport.Storage{Objects: map[string]testsupport.Object{}}
	method := request(t, newHandler(t, storage), http.MethodPost, "/healthz", nil)
	assertJSONError(t, method, http.StatusMethodNotAllowed, "method_not_allowed")
	if method.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("Allow = %q", method.Header().Get("Allow"))
	}
	notFound := request(t, newHandler(t, storage), http.MethodGet, "/", nil)
	assertJSONError(t, notFound, http.StatusNotFound, "release_not_found")
}

func TestOversizedObjectIsRejectedBeforeBodyOpen(t *testing.T) {
	t.Parallel()

	storage := &fixedSnapshotStore{snapshot: blob.Snapshot{
		Size: config.MaxObjectSize + 1,
		ETag: `"etag-v1"`,
	}}
	recorder := request(
		t,
		newHandler(t, storage),
		http.MethodGet,
		downloadPath,
		authenticatedHeaders(),
	)
	assertJSONError(t, recorder, http.StatusRequestEntityTooLarge, "release_too_large")
	if storage.opened {
		t.Fatal("oversized object body was opened")
	}
}

func TestCanceledStorageRequestDoesNotWriteAnErrorBody(t *testing.T) {
	t.Parallel()

	storage := &fixedSnapshotStore{statErr: context.Canceled}
	recorder := request(
		t,
		newHandler(t, storage),
		http.MethodGet,
		downloadPath,
		authenticatedHeaders(),
	)
	if recorder.Body.Len() != 0 {
		t.Fatalf("body = %q, want empty", recorder.Body.String())
	}
}

func TestMidstreamVersionFailureTerminatesWithoutSensitiveLogging(t *testing.T) {
	t.Parallel()

	payload := bytes.Repeat([]byte("x"), int(config.MaxStorageSegment+1))
	storage := &testsupport.Storage{
		Objects: map[string]testsupport.Object{blobName: {Data: payload, ETag: `"etag-v1"`}},
		OpenHook: func(_ context.Context, call testsupport.OpenCall) (io.ReadCloser, error) {
			if call.Offset == 0 {
				return io.NopCloser(bytes.NewReader(payload[:call.Length])), nil
			}
			return nil, blob.ErrChanged
		},
	}
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	recorder := request(
		t,
		httpapi.New(testConfig(t), storage, logger),
		http.MethodGet,
		downloadPath,
		authenticatedHeaders(),
	)
	if recorder.Code != http.StatusOK || int64(recorder.Body.Len()) != config.MaxStorageSegment {
		t.Fatalf("response = %d, bytes = %d", recorder.Code, recorder.Body.Len())
	}
	if !strings.Contains(logs.String(), "stream_failure") {
		t.Fatalf("log = %q", logs.String())
	}
	for _, forbidden := range []string{version, fileName, blobName, oidcIssuer, allowedSubject, `"etag-v1"`} {
		if strings.Contains(logs.String(), forbidden) {
			t.Fatalf("log contains forbidden value %q", forbidden)
		}
	}
}

func TestReaderCloseFailureTerminatesStream(t *testing.T) {
	t.Parallel()

	storage := &testsupport.Storage{
		Objects: map[string]testsupport.Object{blobName: {Data: []byte("abc"), ETag: `"etag-v1"`}},
		OpenHook: func(context.Context, testsupport.OpenCall) (io.ReadCloser, error) {
			return closeErrorReader{Reader: strings.NewReader("abc")}, nil
		},
	}
	recorder := request(
		t,
		httpapi.New(testConfig(t), storage, nil),
		http.MethodGet,
		downloadPath,
		authenticatedHeaders(),
	)
	if recorder.Code != http.StatusOK || recorder.Body.String() != "abc" {
		t.Fatalf("response = %d %q", recorder.Code, recorder.Body.String())
	}
}

type fixedSnapshotStore struct {
	snapshot blob.Snapshot
	statErr  error
	opened   bool
}

func (storage *fixedSnapshotStore) Stat(context.Context, string) (blob.Snapshot, error) {
	return storage.snapshot, storage.statErr
}

func (storage *fixedSnapshotStore) OpenRange(
	context.Context,
	string,
	int64,
	int64,
	string,
) (io.ReadCloser, error) {
	storage.opened = true
	return nil, blob.ErrUnavailable
}

type closeErrorReader struct {
	io.Reader
}

func (closeErrorReader) Close() error {
	return errors.New("synthetic close failure")
}
