package publication

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/SalehElnagar/dar-download-app/internal/distribution"
)

const maxPublicationBatchSize = 10

var (
	ErrPublication    = errors.New("release publication failed")
	repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	containerPattern  = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{1,61}[a-z0-9])?$`)
	commitPattern     = regexp.MustCompile(`^[a-f0-9]{40}$`)
)

type Config struct {
	Repository         string
	ReleasesContainer  string
	ManifestsContainer string
	BatchesContainer   string
	ApplicationOrigin  string
	BatchSize          int
}

type PublishRequest struct {
	RepositoryRoot string
	Source         SourceRelease
	Recipients     []distribution.Recipient
	CommitSHA      string
	CreatedAt      string
}

type ImmutableBytesRequest struct {
	Container   string
	Name        string
	Body        []byte
	ContentType string
}

type ImmutableFileRequest struct {
	Container      string
	Name           string
	FileName       string
	ExpectedDigest string
	ExpectedSize   int64
	ContentType    string
}

type CurrentManifestRequest struct {
	Container      string
	Name           string
	Body           []byte
	ReleaseVersion string
}

type QueueRequest struct {
	MessageID string
	Body      []byte
}

type PublishedBlob struct {
	Reference distribution.BlobReference
	ETag      string
}

type BlobWriter interface {
	PutImmutableBytes(context.Context, ImmutableBytesRequest) (PublishedBlob, error)
	PutImmutableFile(context.Context, ImmutableFileRequest) (PublishedBlob, error)
	PutCurrentManifest(context.Context, CurrentManifestRequest) (PublishedBlob, error)
}

type QueueWriter interface {
	Send(context.Context, QueueRequest) error
}

type Publisher struct {
	config Config
	blobs  BlobWriter
	queue  QueueWriter
}

type PublicationResult struct {
	BatchCount     int    `json:"batch_count"`
	OperationID    string `json:"operation_id"`
	ReleaseID      string `json:"release_id"`
	ReleaseVersion string `json:"release_version"`
}

func NewPublisher(config Config, blobs BlobWriter, queue QueueWriter) (*Publisher, error) {
	if config.BatchSize == 0 {
		config.BatchSize = maxPublicationBatchSize
	}
	if !validPublisherConfig(config) || blobs == nil || queue == nil {
		return nil, ErrPublication
	}
	config.ApplicationOrigin = strings.TrimSuffix(config.ApplicationOrigin, "/")
	return &Publisher{config: config, blobs: blobs, queue: queue}, nil
}

func (publisher *Publisher) Publish(ctx context.Context, request PublishRequest) (PublicationResult, error) {
	plan, err := publisher.plan(request)
	if err != nil || ctx == nil {
		return PublicationResult{}, ErrPublication
	}
	if err := publisher.publishIdentity(ctx, plan); err != nil {
		return PublicationResult{}, err
	}
	dar, err := publisher.publishDAR(ctx, plan)
	if err != nil {
		return PublicationResult{}, err
	}
	manifest, manifestBody, err := publisher.publishManifest(ctx, plan, dar.Reference)
	if err != nil {
		return PublicationResult{}, err
	}
	batches, err := publisher.publishBatches(ctx, plan)
	if err != nil {
		return PublicationResult{}, err
	}
	if err := publisher.enqueueBatches(ctx, plan, manifest.Reference, batches); err != nil {
		return PublicationResult{}, err
	}
	if err := publisher.advanceCurrentManifest(ctx, plan, manifestBody); err != nil {
		return PublicationResult{}, err
	}
	return plan.result(), nil
}

type publicationPlan struct {
	request      PublishRequest
	artifactPath string
	operationID  string
	batches      [][]byte
}

func (publisher *Publisher) plan(request PublishRequest) (publicationPlan, error) {
	if !commitPattern.MatchString(request.CommitSHA) || !validCreatedAt(request.CreatedAt) ||
		len(request.Recipients) == 0 || !validRecipients(request.Recipients) {
		return publicationPlan{}, ErrPublication
	}
	artifactPath, err := verifySourceArtifact(request.RepositoryRoot, request.Source)
	if err != nil {
		return publicationPlan{}, publicationError("SOURCE_ARTIFACT")
	}
	operationID, err := deriveOperationID(publisher.config.Repository, request.CommitSHA, request.Source)
	if err != nil {
		return publicationPlan{}, err
	}
	batches, err := buildBatches(request.Recipients, publisher.config.BatchSize)
	if err != nil {
		return publicationPlan{}, err
	}
	return publicationPlan{
		request: request, artifactPath: artifactPath, operationID: operationID, batches: batches,
	}, nil
}

func validRecipients(recipients []distribution.Recipient) bool {
	for index, recipient := range recipients {
		raw, err := json.Marshal(recipient)
		if err != nil {
			return false
		}
		if _, err := distribution.ParseRecipient(raw); err != nil ||
			index > 0 && recipients[index-1].Email >= recipient.Email {
			return false
		}
	}
	return true
}

func (publisher *Publisher) publishIdentity(ctx context.Context, plan publicationPlan) error {
	body, err := json.Marshal(releaseIdentity{
		CommitSHA: plan.request.CommitSHA, OperationID: plan.operationID,
		Repository: strings.ToLower(publisher.config.Repository), SchemaVersion: "1.0",
		SourceManifest: plan.request.Source,
	})
	if err != nil {
		return publicationError("IDENTITY_ENCODING")
	}
	_, err = publisher.blobs.PutImmutableBytes(ctx, ImmutableBytesRequest{
		Container: publisher.config.ManifestsContainer,
		Name:      "release-identities/" + plan.request.Source.ReleaseID + "/" + plan.request.Source.Version + ".json",
		Body:      body, ContentType: "application/json",
	})
	if err != nil {
		return publicationError("IDENTITY_BLOB")
	}
	return nil
}

func (publisher *Publisher) publishDAR(ctx context.Context, plan publicationPlan) (PublishedBlob, error) {
	source := plan.request.Source
	published, err := publisher.blobs.PutImmutableFile(ctx, ImmutableFileRequest{
		Container: publisher.config.ReleasesContainer, Name: source.Version + "/" + source.DownloadName,
		FileName: plan.artifactPath, ExpectedDigest: source.DARSHA256,
		ExpectedSize: source.DARSize, ContentType: "application/octet-stream",
	})
	if err != nil || published.Reference.SHA256 != source.DARSHA256 ||
		published.Reference.Size != source.DARSize ||
		published.Reference.Container != publisher.config.ReleasesContainer {
		return PublishedBlob{}, publicationError("DAR_BLOB")
	}
	return published, nil
}

func (publisher *Publisher) publishManifest(
	ctx context.Context,
	plan publicationPlan,
	dar distribution.BlobReference,
) (PublishedBlob, []byte, error) {
	source := plan.request.Source
	manifest := distribution.PublishedManifest{
		DAR: dar, DownloadName: source.DownloadName, OperationID: plan.operationID,
		PublishedAt: plan.request.CreatedAt, ReleaseID: source.ReleaseID, ReleaseVersion: source.Version,
		Repository: strings.ToLower(publisher.config.Repository), SchemaVersion: "1.0",
		SourceCommitSHA: plan.request.CommitSHA,
	}
	body, err := json.Marshal(manifest)
	if err != nil {
		return PublishedBlob{}, nil, publicationError("MANIFEST_ENCODING")
	}
	if _, err := distribution.ParsePublishedManifest(body); err != nil {
		return PublishedBlob{}, nil, publicationError("MANIFEST_CONTRACT")
	}
	published, err := publisher.blobs.PutImmutableBytes(ctx, ImmutableBytesRequest{
		Container: publisher.config.ManifestsContainer,
		Name:      "published/" + source.ReleaseID + "/" + plan.operationID + ".json",
		Body:      body, ContentType: "application/json",
	})
	if err != nil {
		return PublishedBlob{}, nil, publicationError("MANIFEST_BLOB")
	}
	return published, body, nil
}

func (publisher *Publisher) publishBatches(
	ctx context.Context,
	plan publicationPlan,
) ([]PublishedBlob, error) {
	published := make([]PublishedBlob, 0, len(plan.batches))
	for index, body := range plan.batches {
		batch, err := publisher.blobs.PutImmutableBytes(ctx, ImmutableBytesRequest{
			Container: publisher.config.BatchesContainer,
			Name:      plan.operationID + "/batch-" + leftPad(index, 4) + ".jsonl",
			Body:      body, ContentType: "application/x-ndjson",
		})
		if err != nil {
			return nil, publicationError("BATCH_BLOB")
		}
		published = append(published, batch)
	}
	return published, nil
}

func (publisher *Publisher) advanceCurrentManifest(
	ctx context.Context,
	plan publicationPlan,
	body []byte,
) error {
	_, err := publisher.blobs.PutCurrentManifest(ctx, CurrentManifestRequest{
		Container: publisher.config.ManifestsContainer,
		Name:      "current/" + plan.request.Source.ReleaseID + ".json",
		Body:      body, ReleaseVersion: plan.request.Source.Version,
	})
	if err != nil {
		return publicationError("CURRENT_MANIFEST")
	}
	return nil
}

func (publisher *Publisher) enqueueBatches(
	ctx context.Context,
	plan publicationPlan,
	manifest distribution.BlobReference,
	batches []PublishedBlob,
) error {
	for index, batch := range batches {
		request, err := publisher.queueRequest(plan, manifest, batch.Reference, index)
		if err != nil {
			return err
		}
		if err := publisher.queue.Send(ctx, request); err != nil {
			return publicationError("MESSAGE_SEND")
		}
	}
	return nil
}

func (publisher *Publisher) queueRequest(
	plan publicationPlan,
	manifest distribution.BlobReference,
	batch distribution.BlobReference,
	index int,
) (QueueRequest, error) {
	source := plan.request.Source
	messageID := plan.operationID + ":" + strconv.Itoa(index)
	message := distribution.QueueMessage{
		ApplicationURL: publisher.config.ApplicationOrigin + "/v1/releases/" + source.Version + "/download/" + source.DownloadName,
		CreatedAt:      plan.request.CreatedAt, Manifest: manifest, MessageID: messageID,
		OperationID: plan.operationID,
		RecipientBatch: distribution.BatchReference{
			BatchCount: len(plan.batches), BatchIndex: index, Container: batch.Container,
			Name: batch.Name, RecipientCount: batchRecipientCount(index, len(plan.request.Recipients), publisher.config.BatchSize),
			SHA256: batch.SHA256, Size: batch.Size, VersionID: batch.VersionID,
		},
		ReleaseID: source.ReleaseID, ReleaseVersion: source.Version, SchemaVersion: "1.0",
		SourceCommitSHA: plan.request.CommitSHA,
	}
	body, err := json.Marshal(message)
	if err != nil {
		return QueueRequest{}, publicationError("MESSAGE_ENCODING")
	}
	if _, err := distribution.ParseQueueMessage(body, messageID); err != nil {
		return QueueRequest{}, publicationError("MESSAGE_CONTRACT")
	}
	return QueueRequest{MessageID: messageID, Body: body}, nil
}

func (plan publicationPlan) result() PublicationResult {
	return PublicationResult{
		BatchCount: len(plan.batches), OperationID: plan.operationID,
		ReleaseID: plan.request.Source.ReleaseID, ReleaseVersion: plan.request.Source.Version,
	}
}

type releaseIdentity struct {
	CommitSHA      string        `json:"commit_sha"`
	OperationID    string        `json:"operation_id"`
	Repository     string        `json:"repository"`
	SchemaVersion  string        `json:"schema_version"`
	SourceManifest SourceRelease `json:"source_manifest"`
}

type operationIntent struct {
	CommitSHA  string        `json:"commit_sha"`
	Manifest   SourceRelease `json:"manifest"`
	Repository string        `json:"repository"`
}

func deriveOperationID(repository, commitSHA string, source SourceRelease) (string, error) {
	body, err := json.Marshal(operationIntent{
		CommitSHA: commitSHA, Manifest: source, Repository: strings.ToLower(repository),
	})
	if err != nil {
		return "", ErrPublication
	}
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:]), nil
}

func buildBatches(recipients []distribution.Recipient, batchSize int) ([][]byte, error) {
	batchCount := (len(recipients) + batchSize - 1) / batchSize
	if batchCount < 1 || batchCount > 10000 {
		return nil, ErrPublication
	}
	batches := make([][]byte, 0, batchCount)
	for start := 0; start < len(recipients); start += batchSize {
		end := min(start+batchSize, len(recipients))
		var body []byte
		for _, recipient := range recipients[start:end] {
			line, err := json.Marshal(recipient)
			if err != nil {
				return nil, ErrPublication
			}
			body = append(body, line...)
			body = append(body, '\n')
		}
		batches = append(batches, body)
	}
	return batches, nil
}

func validPublisherConfig(config Config) bool {
	if !repositoryPattern.MatchString(config.Repository) || config.BatchSize < 1 ||
		config.BatchSize > maxPublicationBatchSize || !validApplicationOrigin(config.ApplicationOrigin) {
		return false
	}
	containers := []string{config.ReleasesContainer, config.ManifestsContainer, config.BatchesContainer}
	seen := make(map[string]struct{}, len(containers))
	for _, container := range containers {
		if !containerPattern.MatchString(container) || strings.Contains(container, "--") {
			return false
		}
		if _, duplicate := seen[container]; duplicate {
			return false
		}
		seen[container] = struct{}{}
	}
	return true
}

func batchRecipientCount(index, total, size int) int {
	return min(size, total-index*size)
}

func validApplicationOrigin(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Hostname() == "" ||
		parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "" && parsed.Path != "/" ||
		parsed.Port() != "" && parsed.Port() != "443" {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return strings.HasSuffix(host, ".azurecontainerapps.io") || strings.HasSuffix(host, ".example.internal")
}

func validCreatedAt(raw string) bool {
	if !strings.HasSuffix(raw, "Z") {
		return false
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	return err == nil && parsed.Location() == time.UTC
}

func leftPad(value, width int) string {
	return strings.Repeat("0", max(width-len(strconv.Itoa(value)), 0)) + strconv.Itoa(value)
}

func publicationError(reason string) error {
	return fmt.Errorf("%w: %s", ErrPublication, reason)
}
