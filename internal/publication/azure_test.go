package publication

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azservicebus"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/service"
	"github.com/SalehElnagar/dar-download-app/internal/distribution"
)

func TestAzureBlobWriterCreatesImmutableVersionedBlob(t *testing.T) {
	t.Parallel()
	body := []byte("immutable")
	digest := sha256.Sum256(body)
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPut || headerValue(request.Header, "If-None-Match") != "*" ||
			headerValue(request.Header, "x-ms-meta-sha256") != hex.EncodeToString(digest[:]) {
			t.Fatalf("request method=%s headers=%v", request.Method, request.Header)
		}
		return azureResponse(http.StatusCreated, map[string]string{
			"ETag": `"etag-1"`, "x-ms-version-id": "version-1",
		}), nil
	})
	client, err := service.NewClientWithNoCredential(
		"https://account.blob.core.windows.net/",
		&service.ClientOptions{ClientOptions: azcore.ClientOptions{Transport: transport}},
	)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := NewAzureBlobWriter(client)
	if err != nil {
		t.Fatal(err)
	}

	published, err := writer.PutImmutableBytes(context.Background(), ImmutableBytesRequest{
		Container: "dar-release-manifests", Name: "published/release/operation.json",
		Body: body, ContentType: "application/json",
	})
	if err != nil {
		t.Fatalf("PutImmutableBytes() error = %v", err)
	}
	if published.Reference.VersionID != "version-1" || published.ETag != `"etag-1"` ||
		published.Reference.SHA256 != hex.EncodeToString(digest[:]) {
		t.Fatalf("published = %#v", published)
	}
}

func headerValue(headers http.Header, expected string) string {
	for name, values := range headers {
		if strings.EqualFold(name, expected) && len(values) > 0 {
			return values[0]
		}
	}
	return ""
}

func TestAzureQueueWriterPreservesCanonicalBrokerIdentity(t *testing.T) {
	t.Parallel()
	sender := &fakeServiceBusSender{}
	writer, err := NewAzureQueueWriter(sender)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"schema_version":"1.0"}`)
	if err := writer.Send(context.Background(), QueueRequest{MessageID: "operation:0", Body: body}); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if sender.message == nil || sender.message.MessageID == nil ||
		*sender.message.MessageID != "operation:0" || sender.message.ContentType == nil ||
		*sender.message.ContentType != "application/json" || sender.message.Subject == nil ||
		*sender.message.Subject != "dar.release.batch.v1" ||
		string(sender.message.Body) != string(body) {
		t.Fatalf("message = %#v", sender.message)
	}
}

func TestAzureBlobWriterReusesExactImmutableBlobAfterPreconditionFailure(t *testing.T) {
	t.Parallel()
	body := []byte("immutable")
	digest := sha256.Sum256(body)
	digestText := hex.EncodeToString(digest[:])
	requests := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		switch requests {
		case 1:
			return azureResponse(http.StatusPreconditionFailed, nil), nil
		case 2:
			if request.Method != http.MethodHead {
				t.Fatalf("method = %s", request.Method)
			}
			return azureResponse(http.StatusOK, map[string]string{
				"Content-Length": "9", "ETag": `"etag-existing"`,
				"x-ms-version-id": "version-existing", "x-ms-meta-sha256": digestText,
			}), nil
		default:
			t.Fatalf("unexpected request %d", requests)
			return nil, nil
		}
	})
	client, err := service.NewClientWithNoCredential(
		"https://account.blob.core.windows.net/",
		&service.ClientOptions{ClientOptions: azcore.ClientOptions{Transport: transport}},
	)
	if err != nil {
		t.Fatal(err)
	}
	writer, _ := NewAzureBlobWriter(client)

	published, err := writer.PutImmutableBytes(context.Background(), ImmutableBytesRequest{
		Container: "dar-release-manifests", Name: "published/release/operation.json",
		Body: body, ContentType: "application/json",
	})
	if err != nil || published.Reference.VersionID != "version-existing" || requests != 2 {
		t.Fatalf("published=%#v err=%v requests=%d", published, err, requests)
	}
}

func TestAzureBlobWriterAdvancesCurrentManifestWithExactETag(t *testing.T) {
	t.Parallel()
	existing := testPublishedManifest(t, "v8.31.1.01", strings.Repeat("a", 64))
	incoming := testPublishedManifest(t, "v8.31.1.02", strings.Repeat("b", 64))
	existingDigest := sha256.Sum256(existing)
	requests := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		switch requests {
		case 1:
			return azureResponse(http.StatusOK, map[string]string{
				"Content-Length": intString(len(existing)), "ETag": `"etag-old"`,
				"x-ms-version-id":  "version-old",
				"x-ms-meta-sha256": hex.EncodeToString(existingDigest[:]),
			}), nil
		case 2:
			response := azureResponse(http.StatusOK, map[string]string{
				"Content-Length": intString(len(existing)),
			})
			response.Body = io.NopCloser(bytes.NewReader(existing))
			return response, nil
		case 3:
			if request.Method != http.MethodPut || headerValue(request.Header, "If-Match") != `"etag-old"` {
				t.Fatalf("method=%s headers=%v", request.Method, request.Header)
			}
			return azureResponse(http.StatusCreated, map[string]string{
				"ETag": `"etag-new"`, "x-ms-version-id": "version-new",
			}), nil
		default:
			t.Fatalf("unexpected request %d", requests)
			return nil, nil
		}
	})
	client, _ := service.NewClientWithNoCredential(
		"https://account.blob.core.windows.net/",
		&service.ClientOptions{ClientOptions: azcore.ClientOptions{Transport: transport}},
	)
	writer, _ := NewAzureBlobWriter(client)

	published, err := writer.PutCurrentManifest(context.Background(), CurrentManifestRequest{
		Container: "dar-release-manifests", Name: "current/release.json",
		Body: incoming, ReleaseVersion: "v8.31.1.02",
	})
	if err != nil || published.Reference.VersionID != "version-new" || requests != 3 {
		t.Fatalf("published=%#v err=%v requests=%d", published, err, requests)
	}
}

func testPublishedManifest(t *testing.T, version, operationID string) []byte {
	t.Helper()
	body, err := json.Marshal(distribution.PublishedManifest{
		DAR: distribution.BlobReference{
			Container: "dar-releases", Name: version + "/dar-" + version[1:] + ".zip",
			SHA256: strings.Repeat("c", 64), Size: 123, VersionID: "dar-version",
		},
		DownloadName: "dar-" + version[1:] + ".zip", OperationID: operationID,
		PublishedAt: "2026-08-17T19:00:00Z", ReleaseID: "dar_distribution_01",
		ReleaseVersion: version, Repository: "salehelnagar/dar-download-app",
		SchemaVersion: "1.0", SourceCommitSHA: strings.Repeat("d", 40),
	})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func intString(value int) string {
	return strconv.Itoa(value)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) Do(request *http.Request) (*http.Response, error) {
	return function(request)
}

type fakeServiceBusSender struct {
	message *azservicebus.Message
}

func (sender *fakeServiceBusSender) SendMessage(
	_ context.Context, message *azservicebus.Message, _ *azservicebus.SendMessageOptions,
) error {
	copyMessage := *message
	copyMessage.Body = append([]byte(nil), message.Body...)
	sender.message = &copyMessage
	return nil
}

func azureResponse(status int, headers map[string]string) *http.Response {
	header := make(http.Header)
	for name, value := range headers {
		header.Set(name, value)
	}
	return &http.Response{
		StatusCode: status, Status: http.StatusText(status), Header: header,
		Body: io.NopCloser(strings.NewReader("")),
	}
}
