package distribution

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	azblob "github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blockblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/service"
)

// AzureBlobStore permits reads only from the two configured immutable source containers.
type AzureBlobStore struct {
	service *service.Client
	allowed map[string]struct{}
}

// NewAzureBlobStore binds exact container names and never accepts a storage key or SAS.
func NewAzureBlobStore(serviceClient *service.Client, allowedContainers ...string) (*AzureBlobStore, error) {
	if serviceClient == nil || len(allowedContainers) == 0 {
		return nil, ErrContract
	}
	allowed := make(map[string]struct{}, len(allowedContainers))
	for _, name := range allowedContainers {
		if !validContainer(name) {
			return nil, ErrContract
		}
		allowed[name] = struct{}{}
	}
	return &AzureBlobStore{service: serviceClient, allowed: allowed}, nil
}

// ReadSmall reads and verifies one exact small Blob version.
func (store *AzureBlobStore) ReadSmall(ctx context.Context, reference BlobReference, maximum int64) ([]byte, error) {
	if !reference.valid() || reference.Size > maximum || maximum < 1 {
		return nil, ErrContract
	}
	response, err := store.download(ctx, reference)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	body, err := readBounded(response.Body, maximum)
	if err != nil || !reference.Matches(body) {
		return nil, ErrContract
	}
	return body, nil
}

// Verify streams one exact Blob version through SHA-256 before any recipient effect.
func (store *AzureBlobStore) Verify(ctx context.Context, reference BlobReference) error {
	response, err := store.download(ctx, reference)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	digest := sha256.New()
	written, err := io.Copy(digest, io.LimitReader(response.Body, reference.Size+1))
	if err != nil || written != reference.Size || hex.EncodeToString(digest.Sum(nil)) != reference.SHA256 {
		return ErrContract
	}
	return nil
}

// Open returns one exact version stream after validating response identity and length.
func (store *AzureBlobStore) Open(ctx context.Context, reference BlobReference) (io.ReadCloser, error) {
	response, err := store.download(ctx, reference)
	if err != nil {
		return nil, err
	}
	return response.Body, nil
}

func (store *AzureBlobStore) download(ctx context.Context, reference BlobReference) (azblob.DownloadStreamResponse, error) {
	if !reference.valid() {
		return azblob.DownloadStreamResponse{}, ErrContract
	}
	if _, allowed := store.allowed[reference.Container]; !allowed {
		return azblob.DownloadStreamResponse{}, ErrContract
	}
	base := store.service.NewContainerClient(reference.Container).NewBlockBlobClient(reference.Name)
	versioned, err := base.WithVersionID(reference.VersionID)
	if err != nil {
		return azblob.DownloadStreamResponse{}, ErrContract
	}
	response, err := versioned.DownloadStream(ctx, nil)
	if err != nil {
		return azblob.DownloadStreamResponse{}, sanitizeAzureError(err)
	}
	if response.Body == nil || response.ContentLength == nil || *response.ContentLength != reference.Size ||
		response.VersionID == nil || *response.VersionID != reference.VersionID {
		if response.Body != nil {
			_ = response.Body.Close()
		}
		return azblob.DownloadStreamResponse{}, ErrContract
	}
	return response, nil
}

// AzureReceiptStore persists canonical receipts with absence/ETag preconditions.
type AzureReceiptStore struct {
	container *container.Client
}

// NewAzureReceiptStore binds one private receipt container.
func NewAzureReceiptStore(containerClient *container.Client) (*AzureReceiptStore, error) {
	if containerClient == nil {
		return nil, ErrContract
	}
	return &AzureReceiptStore{container: containerClient}, nil
}

// Get returns the current canonical receipt and its strong ETag.
func (store *AzureReceiptStore) Get(ctx context.Context, path string) (StoredReceipt, bool, error) {
	if !validBlobName(path) {
		return StoredReceipt{}, false, ErrReceiptConflict
	}
	client := store.container.NewBlockBlobClient(path)
	response, err := client.DownloadStream(ctx, nil)
	if err != nil {
		if azureStatus(err) == http.StatusNotFound {
			return StoredReceipt{}, false, nil
		}
		return StoredReceipt{}, false, sanitizeAzureError(err)
	}
	defer response.Body.Close()
	body, err := readBounded(response.Body, 32*1024)
	if err != nil || response.ContentLength == nil || int64(len(body)) != *response.ContentLength ||
		response.ETag == nil || !metadataDigestMatches(response.Metadata, body) {
		return StoredReceipt{}, false, ErrReceiptConflict
	}
	receipt, err := ParseReceipt(body)
	if err != nil {
		return StoredReceipt{}, false, err
	}
	return StoredReceipt{Receipt: receipt, ETag: string(*response.ETag)}, true, nil
}

// Create succeeds only when the receipt path is absent.
func (store *AzureReceiptStore) Create(ctx context.Context, path string, receipt Receipt) (StoredReceipt, error) {
	star := azcore.ETag("*")
	return store.put(ctx, path, receipt, &azblob.AccessConditions{
		ModifiedAccessConditions: &azblob.ModifiedAccessConditions{IfNoneMatch: &star},
	})
}

// Replace succeeds only against the exact previously observed ETag.
func (store *AzureReceiptStore) Replace(ctx context.Context, path, etag string, receipt Receipt) (StoredReceipt, error) {
	if etag == "" {
		return StoredReceipt{}, ErrReceiptConflict
	}
	condition := azcore.ETag(etag)
	return store.put(ctx, path, receipt, &azblob.AccessConditions{
		ModifiedAccessConditions: &azblob.ModifiedAccessConditions{IfMatch: &condition},
	})
}

func (store *AzureReceiptStore) put(
	ctx context.Context,
	path string,
	receipt Receipt,
	conditions *azblob.AccessConditions,
) (StoredReceipt, error) {
	if !validBlobName(path) {
		return StoredReceipt{}, ErrReceiptConflict
	}
	body, err := MarshalReceipt(receipt)
	if err != nil {
		return StoredReceipt{}, err
	}
	digest := sha256.Sum256(body)
	digestText := hex.EncodeToString(digest[:])
	contentType := "application/json"
	_, err = store.container.NewBlockBlobClient(path).UploadBuffer(
		ctx,
		body,
		&blockblob.UploadBufferOptions{
			AccessConditions: conditions,
			HTTPHeaders:      &azblob.HTTPHeaders{BlobContentType: &contentType},
			Metadata:         map[string]*string{"sha256": &digestText},
			Concurrency:      1,
		},
	)
	if err != nil {
		status := azureStatus(err)
		if status == http.StatusConflict || status == http.StatusPreconditionFailed {
			return StoredReceipt{}, ErrReceiptConflict
		}
		return StoredReceipt{}, sanitizeAzureError(err)
	}
	stored, exists, err := store.Get(ctx, path)
	if err != nil || !exists || stored.Receipt.StateVersion != receipt.StateVersion {
		return StoredReceipt{}, ErrReceiptConflict
	}
	return stored, nil
}

func metadataDigestMatches(metadata map[string]*string, body []byte) bool {
	var value *string
	for name, candidate := range metadata {
		if strings.EqualFold(name, "sha256") {
			value = candidate
			break
		}
	}
	if value == nil {
		return false
	}
	digest := sha256.Sum256(body)
	return *value == hex.EncodeToString(digest[:])
}

func sanitizeAzureError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	status := azureStatus(err)
	if status == http.StatusBadRequest || status == http.StatusNotFound ||
		status == http.StatusConflict || status == http.StatusPreconditionFailed {
		return ErrContract
	}
	return ErrDependency
}

func azureStatus(err error) int {
	var responseError *azcore.ResponseError
	if errors.As(err, &responseError) {
		return responseError.StatusCode
	}
	return 0
}
