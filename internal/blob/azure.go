// Package blob provides the narrow, read-only Azure Blob boundary.
package blob

import (
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	azblob "github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"
	"github.com/SalehElnagar/dar-download-app/internal/config"
)

const StorageAudience = "https://storage.azure.com/"

var (
	ErrNotFound    = errors.New("release object not found")
	ErrChanged     = errors.New("release object changed")
	ErrUnavailable = errors.New("release storage unavailable")
)

// Snapshot is the current authoritative object representation.
type Snapshot struct {
	Size int64
	ETag string
}

// Store is the only storage capability available to the HTTP application.
type Store interface {
	Stat(ctx context.Context, blobName string) (Snapshot, error)
	OpenRange(
		ctx context.Context,
		blobName string,
		offset int64,
		length int64,
		etag string,
	) (io.ReadCloser, error)
}

type sdkBlobClient interface {
	GetProperties(context.Context, *azblob.GetPropertiesOptions) (azblob.GetPropertiesResponse, error)
	DownloadStream(context.Context, *azblob.DownloadStreamOptions) (azblob.DownloadStreamResponse, error)
}

type blobFactory interface {
	Blob(blobName string) sdkBlobClient
}

type realContainer struct {
	client *container.Client
}

func (factory realContainer) Blob(blobName string) sdkBlobClient {
	return factory.client.NewBlobClient(blobName)
}

type azureFactories struct {
	credential func(*azidentity.ManagedIdentityCredentialOptions) (azcore.TokenCredential, error)
	container  func(string, azcore.TokenCredential, *container.ClientOptions) (blobFactory, error)
}

// AzureStore uses exactly one managed identity and an existing private container.
type AzureStore struct {
	container blobFactory
}

// NewAzureStore creates the production storage adapter without a network call.
func NewAzureStore(cfg config.Config) (*AzureStore, error) {
	return newAzureStore(cfg, azureFactories{
		credential: func(options *azidentity.ManagedIdentityCredentialOptions) (azcore.TokenCredential, error) {
			return azidentity.NewManagedIdentityCredential(options)
		},
		container: func(
			url string,
			credential azcore.TokenCredential,
			options *container.ClientOptions,
		) (blobFactory, error) {
			client, err := container.NewClient(url, credential, options)
			if err != nil {
				return nil, err
			}
			return realContainer{client: client}, nil
		},
	})
}

func newAzureStore(cfg config.Config, factories azureFactories) (*AzureStore, error) {
	credential, err := factories.credential(&azidentity.ManagedIdentityCredentialOptions{
		ID: azidentity.ClientID(cfg.ManagedIdentityClientID),
	})
	if err != nil {
		return nil, ErrUnavailable
	}
	containerURL := "https://" + cfg.StorageAccountName + ".blob.core.windows.net/" + cfg.StorageContainer
	containerClient, err := factories.container(
		containerURL,
		credential,
		&container.ClientOptions{Audience: StorageAudience},
	)
	if err != nil {
		return nil, ErrUnavailable
	}
	return &AzureStore{container: containerClient}, nil
}

// Stat returns only size and strong ETag and never exposes Azure response details.
func (store *AzureStore) Stat(ctx context.Context, blobName string) (Snapshot, error) {
	response, err := store.container.Blob(blobName).GetProperties(ctx, nil)
	if err != nil {
		return Snapshot{}, translateError(err)
	}
	if response.ContentLength == nil || *response.ContentLength < 0 || response.ETag == nil {
		return Snapshot{}, ErrUnavailable
	}
	etag := string(*response.ETag)
	if !validStrongETag(etag) {
		return Snapshot{}, ErrUnavailable
	}
	return Snapshot{Size: *response.ContentLength, ETag: etag}, nil
}

// OpenRange opens one bounded ETag-conditioned payload response.
func (store *AzureStore) OpenRange(
	ctx context.Context,
	blobName string,
	offset int64,
	length int64,
	etag string,
) (io.ReadCloser, error) {
	if offset < 0 || offset > config.MaxObjectSize || length < 1 ||
		length > config.MaxStorageSegment || !validStrongETag(etag) {
		return nil, ErrUnavailable
	}
	condition := azcore.ETag(etag)
	response, err := store.container.Blob(blobName).DownloadStream(
		ctx,
		&azblob.DownloadStreamOptions{
			Range: azblob.HTTPRange{Offset: offset, Count: length},
			AccessConditions: &azblob.AccessConditions{
				ModifiedAccessConditions: &azblob.ModifiedAccessConditions{IfMatch: &condition},
			},
		},
	)
	if err != nil {
		return nil, translateError(err)
	}
	if response.Body == nil || response.ContentLength == nil ||
		*response.ContentLength != length || response.ETag == nil ||
		string(*response.ETag) != etag {
		if response.Body != nil {
			_ = response.Body.Close()
		}
		return nil, ErrUnavailable
	}
	return response.Body, nil
}

func translateError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var responseError *azcore.ResponseError
	if errors.As(err, &responseError) {
		switch responseError.StatusCode {
		case http.StatusNotFound:
			return ErrNotFound
		case http.StatusPreconditionFailed:
			return ErrChanged
		}
	}
	return ErrUnavailable
}

func validStrongETag(value string) bool {
	if len(value) < 2 || len(value) > 128 || value[0] != '"' || value[len(value)-1] != '"' {
		return false
	}
	for index := 1; index < len(value)-1; index++ {
		character := value[index]
		if character == '"' || character < 0x21 || character == 0x7f {
			return false
		}
	}
	return true
}
