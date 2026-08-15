package httpapi_test

import (
	"bytes"
	"crypto/sha256"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"

	"github.com/SalehElnagar/dar-download-app/internal/config"
	"github.com/SalehElnagar/dar-download-app/internal/testsupport"
)

func TestThirtyMiBDownloadAndResumeOverHTTP(t *testing.T) {
	runtime.GC()

	payload := make([]byte, 30*1024*1024)
	for index := range payload {
		payload[index] = byte(index % 251)
	}
	storage := &testsupport.Storage{Objects: map[string]testsupport.Object{
		blobName: {Data: payload, ETag: `"etag-30mib"`},
	}}
	server := httptest.NewServer(newHandler(t, storage))
	defer server.Close()

	client := server.Client()
	fullRequest, err := http.NewRequest(
		http.MethodGet,
		server.URL+"/v1/releases/"+releaseID+"/download",
		nil,
	)
	if err != nil {
		t.Fatalf("http.NewRequest() error = %v", err)
	}
	fullRequest.Header = authenticatedHeaders()
	response, err := client.Do(fullRequest)
	if err != nil {
		t.Fatalf("client.Do() error = %v", err)
	}
	fullHash := sha256.New()
	fullLength, readErr := io.Copy(fullHash, response.Body)
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil {
		t.Fatalf("read/close errors = %v / %v", readErr, closeErr)
	}
	expectedFullHash := sha256.Sum256(payload)
	if response.StatusCode != http.StatusOK ||
		fullLength != int64(len(payload)) ||
		!bytes.Equal(fullHash.Sum(nil), expectedFullHash[:]) {
		t.Fatalf("full response status = %d, bytes = %d", response.StatusCode, fullLength)
	}
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	if memory.HeapInuse >= 96*1024*1024 {
		t.Fatalf("conservative integration heap in use = %d bytes, want less than 96 MiB", memory.HeapInuse)
	}

	resumeOffset := int64(17*1024*1024 + 123)
	rangeRequest, err := http.NewRequest(
		http.MethodGet,
		server.URL+"/v1/releases/"+releaseID+"/download",
		nil,
	)
	if err != nil {
		t.Fatalf("http.NewRequest() error = %v", err)
	}
	rangeRequest.Header = authenticatedHeaders()
	rangeRequest.Header.Set("Range", "bytes=17825915-")
	rangeRequest.Header.Set("If-Range", `"etag-30mib"`)
	rangeResponse, err := client.Do(rangeRequest)
	if err != nil {
		t.Fatalf("client.Do(range) error = %v", err)
	}
	rangeHash := sha256.New()
	rangeLength, readErr := io.Copy(rangeHash, rangeResponse.Body)
	closeErr = rangeResponse.Body.Close()
	if readErr != nil || closeErr != nil {
		t.Fatalf("range read/close errors = %v / %v", readErr, closeErr)
	}
	expectedRangeHash := sha256.Sum256(payload[resumeOffset:])
	if rangeResponse.StatusCode != http.StatusPartialContent ||
		rangeLength != int64(len(payload))-resumeOffset ||
		!bytes.Equal(rangeHash.Sum(nil), expectedRangeHash[:]) {
		t.Fatalf("range response status = %d, bytes = %d", rangeResponse.StatusCode, rangeLength)
	}
	_, openCalls, maxActive := storage.Counts()
	for _, call := range openCalls {
		if call.Length > config.MaxStorageSegment {
			t.Fatalf("segment length = %d", call.Length)
		}
	}
	if maxActive != 1 {
		t.Fatalf("max active readers = %d, want 1", maxActive)
	}
}
