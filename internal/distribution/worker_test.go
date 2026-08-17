package distribution

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"
	"time"
)

func TestWorkerAcceptsThenSkipsDuplicateWithoutSecondMailEffect(t *testing.T) {
	t.Parallel()
	fixture := newFixture(t, nil)
	blobs := newFakeBlobs(fixture)
	receipts := newFakeReceipts()
	mailer := &fakeMailer{results: []MailResult{{Outcome: MailSimulated, HTTPStatus: 200, RequestID: "stub-1"}}}
	worker := NewWorker(WorkerOptions{
		Blobs: blobs, Receipts: receipts, Mailer: mailer,
		HMACKey: bytes.Repeat([]byte("k"), 32), HMACKeyVersion: "v1", Provider: "stub",
		Clock: fixedClock(), MaxAttempts: 5, ClaimTimeout: 5 * time.Minute,
	})

	first := worker.Process(context.Background(), fixture.messageID, fixture.messageBody)
	replay := worker.Process(context.Background(), fixture.messageID, fixture.messageBody)

	if first.Disposition != Complete || replay.Disposition != Complete {
		t.Fatalf("results = %#v, %#v", first, replay)
	}
	if first.ProcessedCount != 1 || replay.SkippedCount != 1 || len(mailer.requests) != 1 {
		t.Fatalf("counts = first %#v replay %#v mail=%d", first, replay, len(mailer.requests))
	}
	for path := range receipts.values {
		if stringContains(path, "@") {
			t.Fatalf("receipt path contains email: %q", path)
		}
	}
}

func TestWorkerCommitsUnknownAndDeadLettersAfterAmbiguousMailOutcome(t *testing.T) {
	t.Parallel()
	fixture := newFixture(t, nil)
	receipts := newFakeReceipts()
	mailer := &fakeMailer{results: []MailResult{{Outcome: MailUnknown, ReasonCode: "PROVIDER_OUTCOME_UNKNOWN"}}}
	worker := NewWorker(WorkerOptions{
		Blobs: newFakeBlobs(fixture), Receipts: receipts, Mailer: mailer,
		HMACKey: bytes.Repeat([]byte("k"), 32), HMACKeyVersion: "v1", Provider: "stub",
		Clock: fixedClock(), MaxAttempts: 5, ClaimTimeout: 5 * time.Minute,
	})

	result := worker.Process(context.Background(), fixture.messageID, fixture.messageBody)

	if result.Disposition != DeadLetter || result.ReasonCode != "PROVIDER_OUTCOME_UNKNOWN" {
		t.Fatalf("Process() = %#v", result)
	}
	for _, stored := range receipts.values {
		if stored.Receipt.Status != ReceiptUnknown {
			t.Fatalf("receipt = %#v", stored.Receipt)
		}
	}
}

func TestWorkerValidatesEveryBatchRowBeforeFirstMailEffect(t *testing.T) {
	t.Parallel()
	fixture := newFixture(t, []Recipient{
		{Email: "ava.example@example.com", FirstName: "Ava", LastName: "Example"},
		{Email: "noah.sample@example.com", FirstName: "Noah", LastName: "Sample"},
	})
	fixture.batchBody = []byte("{\"email\":\"ava.example@example.com\",\"first_name\":\"Ava\",\"last_name\":\"Example\"}\n{\"email\":\"invalid\",\"first_name\":\"Noah\",\"last_name\":\"Sample\"}\n")
	updatedReference := reference(fixture.batchRef.Container, fixture.batchRef.Name, fixture.batchBody)
	fixture.batchRef.SHA256 = updatedReference.SHA256
	fixture.batchRef.Size = updatedReference.Size
	var message QueueMessage
	if err := json.Unmarshal(fixture.messageBody, &message); err != nil {
		t.Fatal(err)
	}
	message.RecipientBatch = fixture.batchRef
	fixture.messageBody, _ = json.Marshal(message)
	receipts := newFakeReceipts()
	mailer := &fakeMailer{}
	worker := NewWorker(WorkerOptions{
		Blobs: newFakeBlobs(fixture), Receipts: receipts, Mailer: mailer,
		HMACKey: bytes.Repeat([]byte("k"), 32), HMACKeyVersion: "v1", Provider: "stub",
		Clock: fixedClock(), MaxAttempts: 5, ClaimTimeout: 5 * time.Minute,
	})

	result := worker.Process(context.Background(), fixture.messageID, fixture.messageBody)

	if result.Disposition != DeadLetter || len(mailer.requests) != 0 || len(receipts.values) != 0 {
		t.Fatalf("result=%#v mail=%d receipts=%d", result, len(mailer.requests), len(receipts.values))
	}
}

func TestWorkerLostReceiptCommitBecomesUnknownOnReplayWithoutResend(t *testing.T) {
	t.Parallel()
	fixture := newFixture(t, nil)
	now := time.Date(2026, 8, 17, 19, 0, 0, 0, time.UTC)
	receipts := newFakeReceipts()
	receipts.failReplaceCall = 2
	mailer := &fakeMailer{results: []MailResult{{Outcome: MailSimulated, HTTPStatus: 200, RequestID: "stub-1"}}}
	worker := NewWorker(WorkerOptions{
		Blobs: newFakeBlobs(fixture), Receipts: receipts, Mailer: mailer,
		HMACKey: bytes.Repeat([]byte("k"), 32), HMACKeyVersion: "v1", Provider: "stub",
		Clock: func() time.Time { return now }, MaxAttempts: 5, ClaimTimeout: 5 * time.Minute,
	})

	first := worker.Process(context.Background(), fixture.messageID, fixture.messageBody)
	now = now.Add(6 * time.Minute)
	replay := worker.Process(context.Background(), fixture.messageID, fixture.messageBody)

	if first.ReasonCode != "RECEIPT_COMMIT_UNKNOWN" || replay.ReasonCode != "STALE_SEND_OUTCOME" {
		t.Fatalf("results = %#v, %#v", first, replay)
	}
	if len(mailer.requests) != 1 {
		t.Fatalf("mail requests = %d, want 1", len(mailer.requests))
	}
	for _, stored := range receipts.values {
		if stored.Receipt.Status != ReceiptUnknown {
			t.Fatalf("receipt = %#v", stored.Receipt)
		}
	}
}

func TestWorkerExplicitThrottlePersistsRetryAndDoesNotBusyResend(t *testing.T) {
	t.Parallel()
	fixture := newFixture(t, nil)
	now := time.Date(2026, 8, 17, 19, 0, 0, 0, time.UTC)
	receipts := newFakeReceipts()
	mailer := &fakeMailer{results: []MailResult{
		{Outcome: MailRetryable, HTTPStatus: 429, ReasonCode: "PROVIDER_THROTTLED", RetryAfter: 10 * time.Second},
		{Outcome: MailSimulated, HTTPStatus: 200, RequestID: "stub-2"},
	}}
	worker := NewWorker(WorkerOptions{
		Blobs: newFakeBlobs(fixture), Receipts: receipts, Mailer: mailer,
		HMACKey: bytes.Repeat([]byte("k"), 32), HMACKeyVersion: "v1", Provider: "stub",
		Clock: func() time.Time { return now }, MaxAttempts: 5, ClaimTimeout: 5 * time.Minute,
	})

	first := worker.Process(context.Background(), fixture.messageID, fixture.messageBody)
	immediate := worker.Process(context.Background(), fixture.messageID, fixture.messageBody)
	now = now.Add(first.RetryAfter)
	retried := worker.Process(context.Background(), fixture.messageID, fixture.messageBody)

	if first.Disposition != Abandon || first.RetryAfter < 10*time.Second ||
		immediate.ReasonCode != "RETRY_NOT_DUE" || retried.Disposition != Complete {
		t.Fatalf("results = %#v, %#v, %#v", first, immediate, retried)
	}
	if len(mailer.requests) != 2 {
		t.Fatalf("mail requests = %d, want 2", len(mailer.requests))
	}
}

func TestWorkerAbandonsTransientBlobFailureInsteadOfPoisoningMessage(t *testing.T) {
	t.Parallel()
	fixture := newFixture(t, nil)
	blobs := newFakeBlobs(fixture)
	blobs.verifyErr = ErrDependency
	mailer := &fakeMailer{}
	receipts := newFakeReceipts()
	worker := NewWorker(WorkerOptions{
		Blobs: blobs, Receipts: receipts, Mailer: mailer,
		HMACKey: bytes.Repeat([]byte("k"), 32), HMACKeyVersion: "v1", Provider: "stub",
		Clock: fixedClock(), MaxAttempts: 5, ClaimTimeout: 5 * time.Minute,
	})

	result := worker.Process(context.Background(), fixture.messageID, fixture.messageBody)

	if result.Disposition != Abandon || result.ReasonCode != "BLOB_UNAVAILABLE" ||
		len(mailer.requests) != 0 || len(receipts.values) != 0 {
		t.Fatalf("Process() = %#v", result)
	}
}

type fakeBlobs struct {
	objects   map[string][]byte
	verifyErr error
}

func newFakeBlobs(fixture fixture) *fakeBlobs {
	return &fakeBlobs{objects: map[string][]byte{
		blobKey(fixture.manifestRef):          fixture.manifestBody,
		blobKey(fixture.batchRef.Reference()): fixture.batchBody,
	}}
}

func (fake *fakeBlobs) ReadSmall(_ context.Context, reference BlobReference, maximum int64) ([]byte, error) {
	body, ok := fake.objects[blobKey(reference)]
	if !ok || int64(len(body)) > maximum || !reference.Matches(body) {
		return nil, errors.New("invalid blob")
	}
	return append([]byte(nil), body...), nil
}

func (fake *fakeBlobs) Verify(_ context.Context, reference BlobReference) error {
	if fake.verifyErr != nil {
		return fake.verifyErr
	}
	body, ok := fake.objects[blobKey(reference)]
	if !ok || !reference.Matches(body) {
		return errors.New("invalid blob")
	}
	return nil
}

func (fake *fakeBlobs) Open(_ context.Context, reference BlobReference) (io.ReadCloser, error) {
	body, ok := fake.objects[blobKey(reference)]
	if !ok {
		return nil, errors.New("missing blob")
	}
	return io.NopCloser(bytes.NewReader(body)), nil
}

func blobKey(reference BlobReference) string {
	return reference.Container + "/" + reference.Name + "@" + reference.VersionID
}

type fakeReceipts struct {
	values          map[string]StoredReceipt
	version         int
	replaceCalls    int
	failReplaceCall int
}

func newFakeReceipts() *fakeReceipts {
	return &fakeReceipts{values: make(map[string]StoredReceipt)}
}

func (fake *fakeReceipts) Get(_ context.Context, path string) (StoredReceipt, bool, error) {
	value, ok := fake.values[path]
	return value, ok, nil
}

func (fake *fakeReceipts) Create(_ context.Context, path string, receipt Receipt) (StoredReceipt, error) {
	if _, exists := fake.values[path]; exists {
		return StoredReceipt{}, ErrReceiptConflict
	}
	return fake.save(path, receipt), nil
}

func (fake *fakeReceipts) Replace(_ context.Context, path, etag string, receipt Receipt) (StoredReceipt, error) {
	fake.replaceCalls++
	if fake.replaceCalls == fake.failReplaceCall {
		return StoredReceipt{}, ErrReceiptConflict
	}
	current, exists := fake.values[path]
	if !exists || current.ETag != etag {
		return StoredReceipt{}, ErrReceiptConflict
	}
	return fake.save(path, receipt), nil
}

func (fake *fakeReceipts) save(path string, receipt Receipt) StoredReceipt {
	fake.version++
	stored := StoredReceipt{Receipt: receipt, ETag: string(rune('a' + fake.version))}
	fake.values[path] = stored
	return stored
}

type fakeMailer struct {
	results  []MailResult
	requests []Notification
}

func (fake *fakeMailer) Send(_ context.Context, notification Notification) MailResult {
	fake.requests = append(fake.requests, notification)
	result := fake.results[0]
	fake.results = fake.results[1:]
	return result
}

func fixedClock() func() time.Time {
	now := time.Date(2026, 8, 17, 19, 0, 0, 0, time.UTC)
	return func() time.Time { return now }
}
