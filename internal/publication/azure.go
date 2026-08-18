package publication

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/streaming"
	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azservicebus"
	azblob "github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blockblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/service"
	"github.com/SalehElnagar/dar-download-app/internal/distribution"
)

const maxManifestBytes = int64(64 * 1024)

// AzureBlobWriter uses OAuth and requires Storage Blob versioning for authoritative identities.
type AzureBlobWriter struct {
	service *service.Client
}

// NewAzureBlobWriter binds a credentialed service client without accepting a key or SAS.
func NewAzureBlobWriter(serviceClient *service.Client) (*AzureBlobWriter, error) {
	if serviceClient == nil {
		return nil, ErrPublication
	}
	return &AzureBlobWriter{service: serviceClient}, nil
}

func (writer *AzureBlobWriter) PutImmutableBytes(
	ctx context.Context,
	request ImmutableBytesRequest,
) (PublishedBlob, error) {
	if len(request.Body) < 1 {
		return PublishedBlob{}, ErrPublication
	}
	digest := sha256.Sum256(request.Body)
	return writer.putImmutable(ctx, blobUpload{
		identity: blobIdentity{
			container: request.Container, name: request.Name, size: int64(len(request.Body)),
			digest: hex.EncodeToString(digest[:]), contentType: request.ContentType,
		},
		body: streaming.NopCloser(bytes.NewReader(request.Body)),
	})
}

func (writer *AzureBlobWriter) PutImmutableFile(
	ctx context.Context,
	request ImmutableFileRequest,
) (PublishedBlob, error) {
	fileInfo, err := os.Stat(request.FileName)
	if err != nil || !fileInfo.Mode().IsRegular() || fileInfo.Size() != request.ExpectedSize {
		return PublishedBlob{}, ErrPublication
	}
	actualDigest, err := fileDigest(request.FileName)
	if err != nil || actualDigest != request.ExpectedDigest {
		return PublishedBlob{}, ErrPublication
	}
	file, err := os.Open(request.FileName)
	if err != nil {
		return PublishedBlob{}, ErrPublication
	}
	return writer.putImmutable(ctx, blobUpload{
		identity: blobIdentity{
			container: request.Container, name: request.Name, size: request.ExpectedSize,
			digest: request.ExpectedDigest, contentType: request.ContentType,
		},
		body: file,
	})
}

func (writer *AzureBlobWriter) PutCurrentManifest(
	ctx context.Context,
	request CurrentManifestRequest,
) (PublishedBlob, error) {
	if len(request.Body) < 1 || int64(len(request.Body)) > maxManifestBytes {
		return PublishedBlob{}, ErrPublication
	}
	client := writer.service.NewContainerClient(request.Container).NewBlockBlobClient(request.Name)
	properties, err := client.GetProperties(ctx, nil)
	if err != nil {
		if azureStatus(err) != http.StatusNotFound {
			return PublishedBlob{}, ErrPublication
		}
		return writer.PutImmutableBytes(ctx, ImmutableBytesRequest{
			Container: request.Container, Name: request.Name,
			Body: request.Body, ContentType: "application/json",
		})
	}
	existing, err := writer.downloadCurrent(ctx, client, properties)
	if err != nil {
		return PublishedBlob{}, err
	}
	identity := blobIdentity{container: request.Container, name: request.Name}
	if bytes.Equal(existing, request.Body) {
		return publishedFromProperties(identity, properties)
	}
	manifest, err := distribution.ParsePublishedManifest(existing)
	if err != nil {
		return PublishedBlob{}, ErrPublication
	}
	existingKey, existingOK := productVersionKey(manifest.ReleaseVersion)
	incomingKey, incomingOK := productVersionKey(request.ReleaseVersion)
	if !existingOK || !incomingOK || !versionKeyLess(existingKey, incomingKey) || properties.ETag == nil {
		return PublishedBlob{}, ErrPublication
	}
	etag := *properties.ETag
	conditions := &azblob.AccessConditions{
		ModifiedAccessConditions: &azblob.ModifiedAccessConditions{IfMatch: &etag},
	}
	digest := sha256.Sum256(request.Body)
	upload := blobUpload{
		identity: blobIdentity{
			container: request.Container, name: request.Name, size: int64(len(request.Body)),
			digest: hex.EncodeToString(digest[:]), contentType: "application/json",
		},
		body: streaming.NopCloser(bytes.NewReader(request.Body)), conditions: conditions,
	}
	response, err := writer.upload(ctx, upload)
	if err != nil {
		return PublishedBlob{}, ErrPublication
	}
	return publishedFromUpload(upload.identity, response)
}

type blobIdentity struct {
	container   string
	name        string
	digest      string
	size        int64
	contentType string
}

type blobUpload struct {
	identity   blobIdentity
	body       io.ReadSeekCloser
	conditions *azblob.AccessConditions
}

func (writer *AzureBlobWriter) putImmutable(ctx context.Context, upload blobUpload) (PublishedBlob, error) {
	identity := upload.identity
	if ctx == nil || !containerPattern.MatchString(identity.container) || identity.name == "" ||
		identity.size < 1 || len(identity.digest) != 64 || identity.contentType == "" {
		_ = upload.body.Close()
		return PublishedBlob{}, ErrPublication
	}
	star := azcore.ETag("*")
	upload.conditions = &azblob.AccessConditions{
		ModifiedAccessConditions: &azblob.ModifiedAccessConditions{IfNoneMatch: &star},
	}
	client := writer.service.NewContainerClient(identity.container).NewBlockBlobClient(identity.name)
	response, err := writer.upload(ctx, upload)
	if err == nil {
		return publishedFromUpload(identity, response)
	}
	status := azureStatus(err)
	if status != http.StatusConflict && status != http.StatusPreconditionFailed {
		return PublishedBlob{}, ErrPublication
	}
	properties, propertyErr := client.GetProperties(ctx, nil)
	if propertyErr != nil {
		return PublishedBlob{}, ErrPublication
	}
	published, propertyErr := publishedFromProperties(identity, properties)
	if propertyErr != nil || published.Reference.SHA256 != identity.digest ||
		published.Reference.Size != identity.size {
		return PublishedBlob{}, ErrPublication
	}
	return published, nil
}

func (writer *AzureBlobWriter) upload(ctx context.Context, upload blobUpload) (blockblob.UploadResponse, error) {
	client := writer.service.NewContainerClient(upload.identity.container).NewBlockBlobClient(upload.identity.name)
	contentType := upload.identity.contentType
	digest := upload.identity.digest
	return client.Upload(ctx, upload.body, &blockblob.UploadOptions{
		AccessConditions: upload.conditions,
		HTTPHeaders:      &azblob.HTTPHeaders{BlobContentType: &contentType},
		Metadata:         map[string]*string{"sha256": &digest},
	})
}

func (writer *AzureBlobWriter) downloadCurrent(
	ctx context.Context,
	client *blockblob.Client,
	properties azblob.GetPropertiesResponse,
) ([]byte, error) {
	if properties.ContentLength == nil || *properties.ContentLength < 1 ||
		*properties.ContentLength > maxManifestBytes {
		return nil, ErrPublication
	}
	response, err := client.DownloadStream(ctx, nil)
	if err != nil || response.Body == nil {
		return nil, ErrPublication
	}
	defer response.Body.Close()
	body, err := readBounded(response.Body, maxManifestBytes)
	if err != nil || int64(len(body)) != *properties.ContentLength || !metadataMatches(properties.Metadata, body) {
		return nil, ErrPublication
	}
	return body, nil
}

func publishedFromUpload(
	identity blobIdentity,
	response blockblob.UploadResponse,
) (PublishedBlob, error) {
	if response.ETag == nil || response.VersionID == nil || *response.VersionID == "" {
		return PublishedBlob{}, ErrPublication
	}
	return PublishedBlob{Reference: distribution.BlobReference{
		Container: identity.container, Name: identity.name, SHA256: identity.digest, Size: identity.size,
		VersionID: *response.VersionID,
	}, ETag: string(*response.ETag)}, nil
}

func publishedFromProperties(
	identity blobIdentity,
	properties azblob.GetPropertiesResponse,
) (PublishedBlob, error) {
	if properties.ETag == nil || properties.VersionID == nil || properties.ContentLength == nil ||
		*properties.VersionID == "" || *properties.ContentLength < 1 {
		return PublishedBlob{}, ErrPublication
	}
	digest := metadataValue(properties.Metadata, "sha256")
	if len(digest) != 64 {
		return PublishedBlob{}, ErrPublication
	}
	return PublishedBlob{Reference: distribution.BlobReference{
		Container: identity.container, Name: identity.name, SHA256: digest,
		Size: *properties.ContentLength, VersionID: *properties.VersionID,
	}, ETag: string(*properties.ETag)}, nil
}

func metadataMatches(metadata map[string]*string, body []byte) bool {
	digest := sha256.Sum256(body)
	return metadataValue(metadata, "sha256") == hex.EncodeToString(digest[:])
}

func metadataValue(metadata map[string]*string, expected string) string {
	for name, value := range metadata {
		if strings.EqualFold(name, expected) && value != nil {
			return *value
		}
	}
	return ""
}

func azureStatus(err error) int {
	var responseError *azcore.ResponseError
	if errors.As(err, &responseError) {
		return responseError.StatusCode
	}
	return 0
}

type serviceBusSender interface {
	SendMessage(context.Context, *azservicebus.Message, *azservicebus.SendMessageOptions) error
}

// AzureQueueWriter uses the SDK sender already bound to one configured queue.
type AzureQueueWriter struct {
	sender serviceBusSender
}

func NewAzureQueueWriter(sender serviceBusSender) (*AzureQueueWriter, error) {
	if sender == nil {
		return nil, ErrPublication
	}
	return &AzureQueueWriter{sender: sender}, nil
}

func (writer *AzureQueueWriter) Send(ctx context.Context, request QueueRequest) error {
	if ctx == nil || request.MessageID == "" || len(request.MessageID) > 128 ||
		len(request.Body) < 1 || len(request.Body) > 64*1024 {
		return ErrPublication
	}
	contentType := "application/json"
	subject := "dar.release.batch.v1"
	messageID := request.MessageID
	message := &azservicebus.Message{
		Body: append([]byte(nil), request.Body...), ContentType: &contentType,
		MessageID: &messageID, Subject: &subject,
	}
	if err := writer.sender.SendMessage(ctx, message, nil); err != nil {
		return ErrPublication
	}
	return nil
}
