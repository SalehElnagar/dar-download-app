package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/SalehElnagar/dar-download-app/internal/testsupport"
)

const (
	dynamicVersion   = "v26.8.31.01"
	dynamicZIPName   = "canton_dars.zip"
	dynamicDARName   = "canton_dars.dar"
	dynamicZIPBlob   = dynamicVersion + "/" + dynamicZIPName
	dynamicRoute     = "/v1/releases/" + dynamicVersion + "/download/" + dynamicZIPName
	arbitrarySubject = "customer:any-authenticated-subject"
)

func TestAuthenticatedZIPPathMapsToExactBlob(t *testing.T) {
	t.Parallel()

	storage := &testsupport.Storage{Objects: map[string]testsupport.Object{
		dynamicZIPBlob: {Data: []byte("zip"), ETag: `"zip-etag"`},
	}}
	recorder := request(t, newHandler(t, storage), http.MethodGet, dynamicRoute, authenticatedHeaders())
	if recorder.Code != http.StatusOK || recorder.Body.String() != "zip" {
		t.Fatalf("response = %d %q", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("Content-Disposition") != `attachment; filename="canton_dars.zip"` {
		t.Fatalf("Content-Disposition = %q", recorder.Header().Get("Content-Disposition"))
	}
	statCalls, openCalls, _ := storage.Counts()
	if len(statCalls) != 1 || statCalls[0] != dynamicZIPBlob ||
		len(openCalls) != 1 || openCalls[0].BlobName != dynamicZIPBlob {
		t.Fatalf("storage calls = %#v %#v", statCalls, openCalls)
	}
}

func TestAuthenticatedDARPathMapsToExactBlob(t *testing.T) {
	t.Parallel()

	darBlob := dynamicVersion + "/" + dynamicDARName
	storage := &testsupport.Storage{Objects: map[string]testsupport.Object{
		darBlob: {Data: []byte("dar"), ETag: `"dar-etag"`},
	}}
	recorder := request(
		t,
		newHandler(t, storage),
		http.MethodGet,
		"/v1/releases/"+dynamicVersion+"/download/"+dynamicDARName,
		authenticatedHeaders(),
	)
	if recorder.Code != http.StatusOK || recorder.Body.String() != "dar" {
		t.Fatalf("response = %d %q", recorder.Code, recorder.Body.String())
	}
	statCalls, _, _ := storage.Counts()
	if len(statCalls) != 1 || statCalls[0] != darBlob {
		t.Fatalf("stat calls = %#v", statCalls)
	}
}

func TestAnyValidAuthenticatedSubjectCanDownload(t *testing.T) {
	t.Parallel()

	storage := &testsupport.Storage{Objects: map[string]testsupport.Object{
		dynamicZIPBlob: {Data: []byte("zip"), ETag: `"zip-etag"`},
	}}
	recorder := request(
		t,
		newHandler(t, storage),
		http.MethodGet,
		dynamicRoute,
		testsupport.OIDCHeaders(oidcIssuer, arbitrarySubject),
	)
	if recorder.Code != http.StatusOK || recorder.Body.String() != "zip" {
		t.Fatalf("response = %d %q", recorder.Code, recorder.Body.String())
	}
}

func TestDynamicDownloadAuthenticatesBeforeStorage(t *testing.T) {
	t.Parallel()

	storage := &testsupport.Storage{Objects: map[string]testsupport.Object{
		dynamicZIPBlob: {Data: []byte("zip"), ETag: `"zip-etag"`},
	}}
	recorder := request(t, newHandler(t, storage), http.MethodGet, dynamicRoute, nil)
	assertJSONError(t, recorder, http.StatusUnauthorized, "authentication_required")
	statCalls, openCalls, _ := storage.Counts()
	if len(statCalls) != 0 || len(openCalls) != 0 {
		t.Fatalf("storage calls = %#v %#v", statCalls, openCalls)
	}
}

func TestMissingExactDynamicBlobUsesBoundedNotFound(t *testing.T) {
	t.Parallel()

	storage := &testsupport.Storage{Objects: map[string]testsupport.Object{}}
	recorder := request(t, newHandler(t, storage), http.MethodGet, dynamicRoute, authenticatedHeaders())
	assertJSONError(t, recorder, http.StatusNotFound, "release_not_found")
	statCalls, openCalls, _ := storage.Counts()
	if len(statCalls) != 1 || statCalls[0] != dynamicZIPBlob || len(openCalls) != 0 {
		t.Fatalf("storage calls = %#v %#v", statCalls, openCalls)
	}
}

func TestOldStaticReleaseRouteIsRetired(t *testing.T) {
	t.Parallel()

	storage := &testsupport.Storage{Objects: map[string]testsupport.Object{
		blobName: {Data: []byte("legacy"), ETag: `"legacy-etag"`},
	}}
	recorder := request(
		t,
		newHandler(t, storage),
		http.MethodGet,
		"/v1/releases/"+releaseID+"/download",
		authenticatedHeaders(),
	)
	assertJSONError(t, recorder, http.StatusNotFound, "release_not_found")
	statCalls, openCalls, _ := storage.Counts()
	if len(statCalls) != 0 || len(openCalls) != 0 {
		t.Fatalf("storage calls = %#v %#v", statCalls, openCalls)
	}
}

func TestDynamicPathRejectsUnsafeOrAmbiguousSegments(t *testing.T) {
	t.Parallel()

	maximumVersion := strings.Repeat("v", 96)
	maximumFileName := strings.Repeat("f", 124) + ".zip"
	unsafePaths := map[string]string{
		"empty version":                "/v1/releases//download/file.zip",
		"dot version":                  "/v1/releases/./download/file.zip",
		"dot dot version":              "/v1/releases/../download/file.zip",
		"version slash":                "/v1/releases/version/nested/download/file.zip",
		"version backslash":            `/v1/releases/version\\nested/download/file.zip`,
		"version control":              "/v1/releases/version%0A/download/file.zip",
		"version unicode":              "/v1/releases/v%C3%A9rsion/download/file.zip",
		"version too long":             "/v1/releases/" + maximumVersion + "x/download/file.zip",
		"empty file":                   "/v1/releases/version/download/",
		"dot file":                     "/v1/releases/version/download/.",
		"dot dot file":                 "/v1/releases/version/download/..",
		"dot only file":                "/v1/releases/version/download/...",
		"nested file":                  "/v1/releases/version/download/nested/file.zip",
		"file backslash":               `/v1/releases/version/download/nested\\file.zip`,
		"file control":                 "/v1/releases/version/download/file%0A.zip",
		"file unicode":                 "/v1/releases/version/download/f%C3%AEl%C3%A9.zip",
		"unsafe disposition quote":     "/v1/releases/version/download/file%22.zip",
		"unsafe disposition semicolon": "/v1/releases/version/download/file%3B.zip",
		"file too long":                "/v1/releases/version/download/" + maximumFileName + "x",
		"encoded version separator":    "/v1/releases/version%2Fnested/download/file.zip",
		"encoded file separator":       "/v1/releases/version/download/nested%2Ffile.zip",
		"encoded backslash":            "/v1/releases/version/download/nested%5Cfile.zip",
		"encoded unreserved":           "/v1/releases/%76ersion/download/file.zip",
		"double encoded separator":     "/v1/releases/version/download/nested%252Ffile.zip",
		"query ambiguity":              "/v1/releases/version/download/file.zip?alternate=true",
	}
	for name, path := range unsafePaths {
		name, path := name, path
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			storage := &testsupport.Storage{Objects: map[string]testsupport.Object{}}
			recorder := request(t, newHandler(t, storage), http.MethodGet, path, authenticatedHeaders())
			assertJSONError(t, recorder, http.StatusNotFound, "release_not_found")
			statCalls, openCalls, _ := storage.Counts()
			if len(statCalls) != 0 || len(openCalls) != 0 {
				t.Fatalf("storage calls = %#v %#v", statCalls, openCalls)
			}
		})
	}
}

func TestDynamicPathRejectsMalformedRawEscaping(t *testing.T) {
	t.Parallel()

	storage := &testsupport.Storage{Objects: map[string]testsupport.Object{}}
	handler := newHandler(t, storage)
	requestURL := &url.URL{Path: dynamicRoute, RawPath: "/v1/releases/%ZZ/download/canton_dars.zip"}
	req := &http.Request{
		Method:     http.MethodGet,
		URL:        requestURL,
		RequestURI: requestURL.RawPath,
		Header:     authenticatedHeaders(),
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	assertJSONError(t, recorder, http.StatusNotFound, "release_not_found")
	statCalls, openCalls, _ := storage.Counts()
	if len(statCalls) != 0 || len(openCalls) != 0 {
		t.Fatalf("storage calls = %#v %#v", statCalls, openCalls)
	}
}

func TestDynamicPathAcceptsDocumentedMaximumLengths(t *testing.T) {
	t.Parallel()

	version := strings.Repeat("v", 96)
	fileName := strings.Repeat("f", 124) + ".zip"
	blobName := version + "/" + fileName
	storage := &testsupport.Storage{Objects: map[string]testsupport.Object{
		blobName: {Data: []byte("zip"), ETag: `"zip-etag"`},
	}}
	recorder := request(
		t,
		newHandler(t, storage),
		http.MethodGet,
		"/v1/releases/"+version+"/download/"+fileName,
		authenticatedHeaders(),
	)
	if recorder.Code != http.StatusOK || recorder.Body.String() != "zip" {
		t.Fatalf("response = %d %q", recorder.Code, recorder.Body.String())
	}
}

func TestDynamicRangeAndIfRangeRemainBoundToExactBlob(t *testing.T) {
	t.Parallel()

	storage := &testsupport.Storage{Objects: map[string]testsupport.Object{
		dynamicZIPBlob: {Data: []byte("0123456789"), ETag: `"zip-etag"`},
	}}
	partialHeaders := testsupport.OIDCHeaders(oidcIssuer, arbitrarySubject)
	partialHeaders.Set("Range", "bytes=2-5")
	partialHeaders.Set("If-Range", `"zip-etag"`)
	partial := request(t, newHandler(t, storage), http.MethodGet, dynamicRoute, partialHeaders)
	if partial.Code != http.StatusPartialContent || partial.Body.String() != "2345" ||
		partial.Header().Get("Content-Range") != "bytes 2-5/10" {
		t.Fatalf("partial response = %d %q %#v", partial.Code, partial.Body.String(), partial.Header())
	}

	staleHeaders := testsupport.OIDCHeaders(oidcIssuer, arbitrarySubject)
	staleHeaders.Set("Range", "bytes=2-5")
	staleHeaders.Set("If-Range", `"stale-etag"`)
	full := request(t, newHandler(t, storage), http.MethodGet, dynamicRoute, staleHeaders)
	if full.Code != http.StatusOK || full.Body.String() != "0123456789" ||
		full.Header().Get("Content-Range") != "" {
		t.Fatalf("full response = %d %q %#v", full.Code, full.Body.String(), full.Header())
	}
}

func TestDynamicRouteAllowsOnlyGET(t *testing.T) {
	t.Parallel()

	for _, method := range []string{
		http.MethodHead,
		http.MethodPost,
		http.MethodPut,
		http.MethodPatch,
		http.MethodDelete,
		http.MethodOptions,
	} {
		method := method
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			recorder := request(
				t,
				newHandler(t, &testsupport.Storage{Objects: map[string]testsupport.Object{}}),
				method,
				dynamicRoute,
				authenticatedHeaders(),
			)
			assertJSONError(t, recorder, http.StatusMethodNotAllowed, "method_not_allowed")
			if recorder.Header().Get("Allow") != http.MethodGet {
				t.Fatalf("Allow = %q", recorder.Header().Get("Allow"))
			}
		})
	}
}

func TestAzureContainerAppsAllowsAnyValidAuthenticatedSubject(t *testing.T) {
	t.Parallel()

	const differentObjectID = "22222222-2222-4222-8222-222222222222"
	storage := &testsupport.Storage{Objects: map[string]testsupport.Object{
		dynamicZIPBlob: {Data: []byte("zip"), ETag: `"zip-etag"`},
	}}
	recorder := request(
		t,
		newAzureBoundaryHandler(t, storage),
		http.MethodGet,
		dynamicRoute,
		azureBoundaryHeaders(t, azureBoundaryTenant, differentObjectID),
	)
	if recorder.Code != http.StatusOK || recorder.Body.String() != "zip" {
		t.Fatalf("response = %d %q", recorder.Code, recorder.Body.String())
	}
}

func TestDynamicResponseLengthMatchesPayload(t *testing.T) {
	t.Parallel()

	storage := &testsupport.Storage{Objects: map[string]testsupport.Object{
		dynamicZIPBlob: {Data: []byte("zip"), ETag: `"zip-etag"`},
	}}
	recorder := request(t, newHandler(t, storage), http.MethodGet, dynamicRoute, authenticatedHeaders())
	if recorder.Header().Get("Content-Length") != strconv.Itoa(recorder.Body.Len()) {
		t.Fatalf("Content-Length = %q, body bytes = %d", recorder.Header().Get("Content-Length"), recorder.Body.Len())
	}
}
