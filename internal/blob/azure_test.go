package blob

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	azblob "github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"
	"github.com/SalehElnagar/dar-download-app/internal/config"
)

const (
	testTenantID          = "11111111-1111-4111-8111-111111111111"
	testManagedIdentityID = "22222222-2222-4222-8222-222222222222"
	testPrincipalID       = "33333333-3333-4333-8333-333333333333"
	testBlobName          = "releases/2026-08/example.dar"
)

type fakeCredential struct{}

func (fakeCredential) GetToken(context.Context, policy.TokenRequestOptions) (azcore.AccessToken, error) {
	return azcore.AccessToken{Token: "synthetic", ExpiresOn: time.Now().Add(time.Minute)}, nil
}

type fakeSDKBlob struct {
	properties      azblob.GetPropertiesResponse
	propertiesErr   error
	download        azblob.DownloadStreamResponse
	downloadErr     error
	propertiesCalls int
	downloadCalls   int
	downloadOptions *azblob.DownloadStreamOptions
	lastContext     context.Context
}

func (client *fakeSDKBlob) GetProperties(
	ctx context.Context,
	_ *azblob.GetPropertiesOptions,
) (azblob.GetPropertiesResponse, error) {
	client.propertiesCalls++
	client.lastContext = ctx
	return client.properties, client.propertiesErr
}

func (client *fakeSDKBlob) DownloadStream(
	ctx context.Context,
	options *azblob.DownloadStreamOptions,
) (azblob.DownloadStreamResponse, error) {
	client.downloadCalls++
	client.downloadOptions = options
	client.lastContext = ctx
	return client.download, client.downloadErr
}

type fakeContainer struct {
	client    sdkBlobClient
	blobNames []string
}

func (factory *fakeContainer) Blob(blobName string) sdkBlobClient {
	factory.blobNames = append(factory.blobNames, blobName)
	return factory.client
}

func testConfig(t *testing.T) config.Config {
	t.Helper()
	cfg, err := config.ParseEnvironment(map[string]string{
		config.TenantIDEnv:                testTenantID,
		config.StorageAccountNameEnv:      "stdardownloadpoc01",
		config.StorageContainerEnv:        "dar-releases",
		config.ManagedIdentityClientIDEnv: testManagedIdentityID,
		config.ReleasesJSONEnv: `{"dar_01JABCDEF0123456789XYZ":{` +
			`"allowed_principal_ids":["` + testPrincipalID + `"],` +
			`"blob_name":"` + testBlobName + `","download_name":"example.dar"}}`,
	})
	if err != nil {
		t.Fatalf("config.ParseEnvironment() error = %v", err)
	}
	return cfg
}

func TestNewAzureStoreUsesExactIdentityEndpointAndAudience(t *testing.T) {
	t.Parallel()

	var credentialOptions *azidentity.ManagedIdentityCredentialOptions
	var containerURL string
	var containerOptions *container.ClientOptions
	factory := &fakeContainer{client: &fakeSDKBlob{}}

	store, err := newAzureStore(testConfig(t), azureFactories{
		credential: func(options *azidentity.ManagedIdentityCredentialOptions) (azcore.TokenCredential, error) {
			credentialOptions = options
			return fakeCredential{}, nil
		},
		container: func(
			url string,
			_ azcore.TokenCredential,
			options *container.ClientOptions,
		) (blobFactory, error) {
			containerURL = url
			containerOptions = options
			return factory, nil
		},
	})
	if err != nil {
		t.Fatalf("newAzureStore() error = %v", err)
	}
	if store == nil {
		t.Fatal("newAzureStore() returned nil")
	}
	clientID, ok := credentialOptions.ID.(azidentity.ClientID)
	if !ok || clientID.String() != testManagedIdentityID {
		t.Fatalf("managed identity ID = %#v", credentialOptions.ID)
	}
	if containerURL != "https://stdardownloadpoc01.blob.core.windows.net/dar-releases" {
		t.Fatalf("container URL = %q", containerURL)
	}
	if containerOptions.Audience != StorageAudience {
		t.Fatalf("audience = %q", containerOptions.Audience)
	}
}

func TestNewAzureStoreBuildsProductionClientsWithoutNetworkAccess(t *testing.T) {
	t.Parallel()

	store, err := NewAzureStore(testConfig(t))
	if err != nil || store == nil {
		t.Fatalf("NewAzureStore() = %#v, %v", store, err)
	}
	if store.container.Blob(testBlobName) == nil {
		t.Fatal("real container returned a nil Blob client")
	}
}

func TestNewAzureStoreRejectsFactoryFailures(t *testing.T) {
	t.Parallel()

	wantFailure := errors.New("synthetic constructor failure")
	tests := []struct {
		name      string
		factories azureFactories
	}{
		{
			name: "credential",
			factories: azureFactories{
				credential: func(*azidentity.ManagedIdentityCredentialOptions) (azcore.TokenCredential, error) {
					return nil, wantFailure
				},
			},
		},
		{
			name: "container",
			factories: azureFactories{
				credential: func(*azidentity.ManagedIdentityCredentialOptions) (azcore.TokenCredential, error) {
					return fakeCredential{}, nil
				},
				container: func(string, azcore.TokenCredential, *container.ClientOptions) (blobFactory, error) {
					return nil, wantFailure
				},
			},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			store, err := newAzureStore(testConfig(t), tt.factories)
			if store != nil || !errors.Is(err, ErrUnavailable) {
				t.Fatalf("newAzureStore() = %#v, %v", store, err)
			}
		})
	}
}

func TestAzureStoreStatsExactConfiguredBlob(t *testing.T) {
	t.Parallel()

	size := int64(30 * 1024 * 1024)
	etag := azcore.ETag(`"etag-v1"`)
	client := &fakeSDKBlob{properties: azblob.GetPropertiesResponse{
		ContentLength: &size,
		ETag:          &etag,
	}}
	factory := &fakeContainer{client: client}
	store := &AzureStore{container: factory}

	snapshot, err := store.Stat(context.Background(), testBlobName)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if snapshot.Size != size || snapshot.ETag != `"etag-v1"` {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if client.propertiesCalls != 1 || len(factory.blobNames) != 1 || factory.blobNames[0] != testBlobName {
		t.Fatalf("calls = %d, names = %#v", client.propertiesCalls, factory.blobNames)
	}
}

func TestAzureStoreOpensExactETagBoundSegment(t *testing.T) {
	t.Parallel()

	length := int64(4)
	etag := azcore.ETag(`"etag-v1"`)
	client := &fakeSDKBlob{download: azblob.DownloadStreamResponse{
		DownloadResponse: azblob.DownloadResponse{
			Body:          io.NopCloser(strings.NewReader("2345")),
			ContentLength: &length,
			ETag:          &etag,
		},
	}}
	factory := &fakeContainer{client: client}
	store := &AzureStore{container: factory}

	reader, err := store.OpenRange(context.Background(), testBlobName, 2, length, `"etag-v1"`)
	if err != nil {
		t.Fatalf("OpenRange() error = %v", err)
	}
	defer reader.Close()
	payload, readErr := io.ReadAll(reader)
	if readErr != nil || string(payload) != "2345" {
		t.Fatalf("payload = %q, error = %v", payload, readErr)
	}
	options := client.downloadOptions
	if options == nil || options.Range.Offset != 2 || options.Range.Count != length {
		t.Fatalf("range options = %#v", options)
	}
	if options.AccessConditions == nil || options.AccessConditions.ModifiedAccessConditions == nil ||
		options.AccessConditions.ModifiedAccessConditions.IfMatch == nil ||
		string(*options.AccessConditions.ModifiedAccessConditions.IfMatch) != `"etag-v1"` {
		t.Fatalf("access conditions = %#v", options.AccessConditions)
	}
}

func TestAzureStoreRejectsUnboundedOrUnsafeOpenWithoutSDKCall(t *testing.T) {
	t.Parallel()

	client := &fakeSDKBlob{}
	store := &AzureStore{container: &fakeContainer{client: client}}
	for _, request := range []struct {
		offset int64
		length int64
		etag   string
	}{
		{offset: -1, length: 1, etag: `"etag-v1"`},
		{offset: 0, length: 0, etag: `"etag-v1"`},
		{offset: 0, length: config.MaxStorageSegment + 1, etag: `"etag-v1"`},
		{offset: 0, length: 1, etag: "weak"},
	} {
		if _, err := store.OpenRange(context.Background(), testBlobName, request.offset, request.length, request.etag); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("OpenRange(%#v) error = %v", request, err)
		}
	}
	if client.downloadCalls != 0 {
		t.Fatalf("download calls = %d, want 0", client.downloadCalls)
	}
}

func TestAzureStoreMapsBoundedErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		statusCode int
		want       error
	}{
		{name: "not found", statusCode: http.StatusNotFound, want: ErrNotFound},
		{name: "changed", statusCode: http.StatusPreconditionFailed, want: ErrChanged},
		{name: "unavailable", statusCode: http.StatusServiceUnavailable, want: ErrUnavailable},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := &fakeSDKBlob{propertiesErr: &azcore.ResponseError{StatusCode: tt.statusCode}}
			store := &AzureStore{container: &fakeContainer{client: client}}
			if _, err := store.Stat(context.Background(), testBlobName); !errors.Is(err, tt.want) {
				t.Fatalf("Stat() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestAzureStoreRejectsInvalidObjectMetadata(t *testing.T) {
	t.Parallel()

	negative := int64(-1)
	validSize := int64(1)
	weak := azcore.ETag(`W/"etag-v1"`)
	tests := []azblob.GetPropertiesResponse{
		{},
		{ContentLength: &negative},
		{ContentLength: &validSize},
		{ContentLength: &validSize, ETag: &weak},
	}
	for index, properties := range tests {
		client := &fakeSDKBlob{properties: properties}
		store := &AzureStore{container: &fakeContainer{client: client}}
		if _, err := store.Stat(context.Background(), testBlobName); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("case %d Stat() error = %v", index, err)
		}
	}
}

func TestAzureStoreMapsDownloadFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status int
		want   error
	}{
		{status: http.StatusNotFound, want: ErrNotFound},
		{status: http.StatusPreconditionFailed, want: ErrChanged},
		{status: http.StatusInternalServerError, want: ErrUnavailable},
	}
	for _, tt := range tests {
		client := &fakeSDKBlob{downloadErr: &azcore.ResponseError{StatusCode: tt.status}}
		store := &AzureStore{container: &fakeContainer{client: client}}
		if _, err := store.OpenRange(context.Background(), testBlobName, 0, 1, `"etag-v1"`); !errors.Is(err, tt.want) {
			t.Fatalf("status %d error = %v, want %v", tt.status, err, tt.want)
		}
	}
}

func TestAzureStorePreservesContextCancellation(t *testing.T) {
	t.Parallel()

	client := &fakeSDKBlob{propertiesErr: context.Canceled}
	store := &AzureStore{container: &fakeContainer{client: client}}
	if _, err := store.Stat(context.Background(), testBlobName); !errors.Is(err, context.Canceled) {
		t.Fatalf("Stat() error = %v, want context.Canceled", err)
	}
}

func TestAzureStoreRejectsMismatchedResponseMetadataAndClosesBody(t *testing.T) {
	t.Parallel()

	length := int64(3)
	etag := azcore.ETag(`"etag-v1"`)
	body := &closeTracker{Reader: strings.NewReader("abc")}
	client := &fakeSDKBlob{download: azblob.DownloadStreamResponse{
		DownloadResponse: azblob.DownloadResponse{
			Body:          body,
			ContentLength: &length,
			ETag:          &etag,
		},
	}}
	store := &AzureStore{container: &fakeContainer{client: client}}
	if _, err := store.OpenRange(context.Background(), testBlobName, 0, 4, `"etag-v1"`); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("OpenRange() error = %v, want ErrUnavailable", err)
	}
	if !body.closed {
		t.Fatal("response body was not closed")
	}
}

type closeTracker struct {
	io.Reader
	closed bool
}

func (tracker *closeTracker) Close() error {
	tracker.closed = true
	return nil
}
