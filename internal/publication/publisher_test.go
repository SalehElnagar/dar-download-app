package publication

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SalehElnagar/dar-download-app/internal/distribution"
)

func TestPublishPersistsEveryImmutableInputBeforePIIFreeQueueEffects(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeReleaseZIP(t, root, "v8.31.1.01", zipEntry{name: "product.dar", body: []byte("dar")})
	source, err := DiscoverSource(root, "dar_distribution_01")
	if err != nil {
		t.Fatal(err)
	}
	recipients, err := ParseRecipients(strings.NewReader(
		"first_name,last_name,email\n" +
			"Ava,Example,ava@example.com\n" +
			"Noah,Sample,noah@example.com\n" +
			"Mia,Demo,mia@example.com\n",
	))
	if err != nil {
		t.Fatal(err)
	}
	blobs := newFakeBlobWriter()
	queue := &fakeQueueWriter{blobs: blobs}
	publisher, err := NewPublisher(Config{
		Repository: "SalehElnagar/dar-download-app", ReleasesContainer: "dar-releases",
		ManifestsContainer: "dar-release-manifests", BatchesContainer: "dar-recipient-batches",
		ApplicationOrigin: "https://download.example.internal", BatchSize: 2,
	}, blobs, queue)
	if err != nil {
		t.Fatal(err)
	}

	result, err := publisher.Publish(context.Background(), PublishRequest{
		RepositoryRoot: root, Source: source, Recipients: recipients,
		CommitSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", CreatedAt: "2026-08-17T19:00:00Z",
	})
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if result.BatchCount != 2 || len(queue.messages) != 2 {
		t.Fatalf("result=%#v messages=%d", result, len(queue.messages))
	}
	for _, message := range queue.messages {
		if strings.Contains(string(message.body), "@") ||
			message.blobCountAtSend != 3+result.BatchCount {
			t.Fatalf("queue message leaked PII or preceded Blob publication: %s", message.body)
		}
		parsed, parseErr := distribution.ParseQueueMessage(message.body, message.id)
		if parseErr != nil || parsed.OperationID != result.OperationID {
			t.Fatalf("ParseQueueMessage() = %#v, %v", parsed, parseErr)
		}
	}
	manifestKey := blobKey{"dar-release-manifests", "published/dar_distribution_01/" + result.OperationID + ".json"}
	manifest, ok := blobs.objects[manifestKey]
	if !ok {
		t.Fatalf("published manifest %v missing", manifestKey)
	}
	if _, err := distribution.ParsePublishedManifest(manifest); err != nil {
		t.Fatalf("ParsePublishedManifest() error = %v", err)
	}
}

func TestNewPublisherRejectsBatchBeyondWorkerLockBudget(t *testing.T) {
	t.Parallel()
	blobs := newFakeBlobWriter()
	queue := &fakeQueueWriter{blobs: blobs}
	if _, err := NewPublisher(Config{
		Repository: "SalehElnagar/dar-download-app", ReleasesContainer: "dar-releases",
		ManifestsContainer: "dar-release-manifests", BatchesContainer: "dar-recipient-batches",
		ApplicationOrigin: "https://download.example.internal", BatchSize: 11,
	}, blobs, queue); err == nil {
		t.Fatal("NewPublisher() accepted a batch that can exceed the worker lock-renewal budget")
	}
}

func TestPublishExactReplayReusesOperationAndMessageIdentity(t *testing.T) {
	t.Parallel()
	fixture := newPublisherFixture(t)
	request := PublishRequest{
		RepositoryRoot: fixture.root, Source: fixture.source, Recipients: fixture.recipients,
		CommitSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", CreatedAt: "2026-08-17T19:00:00Z",
	}
	first, err := fixture.publisher.Publish(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := fixture.publisher.Publish(context.Background(), request)
	if err != nil || replay.OperationID != first.OperationID || len(fixture.queue.messages) != 2 {
		t.Fatalf("replay=%#v err=%v messages=%d", replay, err, len(fixture.queue.messages))
	}
}

func TestPublishChangedSameVersionIntentStopsBeforeSecondQueueEffect(t *testing.T) {
	t.Parallel()
	fixture := newPublisherFixture(t)
	firstRequest := PublishRequest{
		RepositoryRoot: fixture.root, Source: fixture.source, Recipients: fixture.recipients,
		CommitSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", CreatedAt: "2026-08-17T19:00:00Z",
	}
	if _, err := fixture.publisher.Publish(context.Background(), firstRequest); err != nil {
		t.Fatal(err)
	}
	writeReleaseZIP(
		t, fixture.root, "v8.31.1.01", zipEntry{name: "product.dar", body: []byte("changed")},
	)
	changed, err := DiscoverSource(fixture.root, "dar_distribution_01")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.publisher.Publish(context.Background(), PublishRequest{
		RepositoryRoot: fixture.root, Source: changed, Recipients: fixture.recipients,
		CommitSHA: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", CreatedAt: "2026-08-17T19:00:00Z",
	}); err == nil {
		t.Fatal("Publish() accepted a changed artifact intent")
	}
	if len(fixture.queue.messages) != 1 {
		t.Fatal("changed intent produced a queue effect")
	}
}

func TestPublishQueueFailureLeavesCompleteIntentForSafeReplay(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeReleaseZIP(t, root, "v8.31.1.01", zipEntry{name: "product.dar", body: []byte("dar")})
	source, err := DiscoverSource(root, "dar_distribution_01")
	if err != nil {
		t.Fatal(err)
	}
	recipients, err := ParseRecipients(strings.NewReader(
		"first_name,last_name,email\n" +
			"Ava,Example,ava@example.com\n" +
			"Mia,Demo,mia@example.com\n" +
			"Noah,Sample,noah@example.com\n",
	))
	if err != nil {
		t.Fatal(err)
	}
	blobs := newFakeBlobWriter()
	queue := &fakeQueueWriter{blobs: blobs, failAt: 1, failEnabled: true}
	publisher, err := NewPublisher(Config{
		Repository: "SalehElnagar/dar-download-app", ReleasesContainer: "dar-releases",
		ManifestsContainer: "dar-release-manifests", BatchesContainer: "dar-recipient-batches",
		ApplicationOrigin: "https://download.example.internal", BatchSize: 2,
	}, blobs, queue)
	if err != nil {
		t.Fatal(err)
	}
	request := PublishRequest{
		RepositoryRoot: root, Source: source, Recipients: recipients,
		CommitSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", CreatedAt: "2026-08-17T19:00:00Z",
	}

	if _, err := publisher.Publish(context.Background(), request); err == nil {
		t.Fatal("Publish() accepted a partial queue send")
	}
	committedObjects := len(blobs.objects)
	currentKey := blobKey{"dar-release-manifests", "current/dar_distribution_01.json"}
	_, currentAdvanced := blobs.objects[currentKey]
	if len(queue.messages) != 1 || committedObjects != 5 || currentAdvanced {
		t.Fatalf("messages=%d blobs=%d", len(queue.messages), committedObjects)
	}
	queue.failEnabled = false
	if _, err := publisher.Publish(context.Background(), request); err != nil {
		t.Fatalf("replay error = %v", err)
	}
	if len(blobs.objects) != committedObjects+1 || len(queue.messages) != 3 ||
		queue.messages[0].id != queue.messages[1].id {
		t.Fatalf("messages=%#v blobs=%d", queue.messages, len(blobs.objects))
	}
}

type publisherFixture struct {
	root       string
	source     SourceRelease
	recipients []distribution.Recipient
	publisher  *Publisher
	queue      *fakeQueueWriter
}

func newPublisherFixture(t *testing.T) publisherFixture {
	t.Helper()
	root := t.TempDir()
	writeReleaseZIP(t, root, "v8.31.1.01", zipEntry{name: "product.dar", body: []byte("dar")})
	source, err := DiscoverSource(root, "dar_distribution_01")
	if err != nil {
		t.Fatal(err)
	}
	recipients, err := ParseRecipients(strings.NewReader(
		"first_name,last_name,email\nAva,Example,ava@example.com\n",
	))
	if err != nil {
		t.Fatal(err)
	}
	blobs := newFakeBlobWriter()
	queue := &fakeQueueWriter{blobs: blobs}
	publisher, err := NewPublisher(Config{
		Repository: "SalehElnagar/dar-download-app", ReleasesContainer: "dar-releases",
		ManifestsContainer: "dar-release-manifests", BatchesContainer: "dar-recipient-batches",
		ApplicationOrigin: "https://download.example.internal", BatchSize: 10,
	}, blobs, queue)
	if err != nil {
		t.Fatal(err)
	}
	return publisherFixture{
		root: root, source: source, recipients: recipients, publisher: publisher, queue: queue,
	}
}

type blobKey struct {
	container string
	name      string
}

type fakeBlobWriter struct {
	objects map[blobKey][]byte
}

func newFakeBlobWriter() *fakeBlobWriter {
	return &fakeBlobWriter{objects: make(map[blobKey][]byte)}
}

func (writer *fakeBlobWriter) PutImmutableBytes(
	_ context.Context, request ImmutableBytesRequest,
) (PublishedBlob, error) {
	return writer.put(request.Container, request.Name, request.Body)
}

func (writer *fakeBlobWriter) PutImmutableFile(
	_ context.Context, request ImmutableFileRequest,
) (PublishedBlob, error) {
	body, err := os.ReadFile(filepath.Clean(request.FileName))
	if err != nil || int64(len(body)) != request.ExpectedSize {
		return PublishedBlob{}, ErrPublication
	}
	digest := sha256.Sum256(body)
	if hex.EncodeToString(digest[:]) != request.ExpectedDigest {
		return PublishedBlob{}, ErrPublication
	}
	return writer.put(request.Container, request.Name, body)
}

func (writer *fakeBlobWriter) PutCurrentManifest(
	_ context.Context, request CurrentManifestRequest,
) (PublishedBlob, error) {
	key := blobKey{request.Container, request.Name}
	if existing, ok := writer.objects[key]; ok && bytes.Equal(existing, request.Body) {
		return writer.reference(key, request.Body), nil
	}
	writer.objects[key] = append([]byte(nil), request.Body...)
	return writer.reference(key, request.Body), nil
}

func (writer *fakeBlobWriter) put(container, name string, body []byte) (PublishedBlob, error) {
	key := blobKey{container, name}
	if existing, ok := writer.objects[key]; ok {
		if !bytes.Equal(existing, body) {
			return PublishedBlob{}, ErrPublication
		}
		return writer.reference(key, body), nil
	}
	writer.objects[key] = append([]byte(nil), body...)
	return writer.reference(key, body), nil
}

func (writer *fakeBlobWriter) reference(key blobKey, body []byte) PublishedBlob {
	digest := sha256.Sum256(body)
	return PublishedBlob{Reference: distribution.BlobReference{
		Container: key.container, Name: key.name, SHA256: hex.EncodeToString(digest[:]),
		Size: int64(len(body)), VersionID: "version-1",
	}, ETag: `"etag-1"`}
}

type queuedMessage struct {
	id              string
	body            []byte
	blobCountAtSend int
}

type fakeQueueWriter struct {
	blobs       *fakeBlobWriter
	messages    []queuedMessage
	failAt      int
	failEnabled bool
}

func (writer *fakeQueueWriter) Send(_ context.Context, request QueueRequest) error {
	if request.MessageID == "" || len(request.Body) == 0 {
		return errors.New("invalid message")
	}
	if writer.failEnabled && len(writer.messages) == writer.failAt {
		return errors.New("queue unavailable")
	}
	writer.messages = append(writer.messages, queuedMessage{
		id: request.MessageID, body: append([]byte(nil), request.Body...),
		blobCountAtSend: len(writer.blobs.objects),
	})
	return nil
}
