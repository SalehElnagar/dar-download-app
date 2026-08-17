package distribution

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
)

func TestParseQueueMessageAcceptsCanonicalReferenceOnlyContract(t *testing.T) {
	t.Parallel()
	fixture := newFixture(t, []Recipient{{
		Email:     "ava.example@example.com",
		FirstName: "Ava",
		LastName:  "Example",
	}})

	message, err := ParseQueueMessage(fixture.messageBody, fixture.messageID)
	if err != nil {
		t.Fatalf("ParseQueueMessage() error = %v", err)
	}
	if message.ReleaseID != fixture.releaseID || message.RecipientBatch.RecipientCount != 1 {
		t.Fatalf("ParseQueueMessage() = %#v", message)
	}
	if containsPII(fixture.messageBody) {
		t.Fatal("queue message contains recipient PII")
	}
}

func TestWorkerContractsAcceptZIPReleaseArtifact(t *testing.T) {
	t.Parallel()
	fixture := newFixtureForExtension(t, nil, "zip")

	if _, err := ParseQueueMessage(fixture.messageBody, fixture.messageID); err != nil {
		t.Fatalf("ParseQueueMessage() ZIP error = %v", err)
	}
	if _, err := ParsePublishedManifest(fixture.manifestBody); err != nil {
		t.Fatalf("ParsePublishedManifest() ZIP error = %v", err)
	}
}

func TestParseQueueMessageRejectsPIIAndNoncanonicalJSON(t *testing.T) {
	t.Parallel()
	fixture := newFixture(t, nil)

	var payload map[string]any
	if err := json.Unmarshal(fixture.messageBody, &payload); err != nil {
		t.Fatal(err)
	}
	payload["email"] = "private@example.com"
	piiBody, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseQueueMessage(piiBody, fixture.messageID); err == nil {
		t.Fatal("ParseQueueMessage() accepted a PII-bearing message")
	}

	noncanonical := append([]byte(" "), fixture.messageBody...)
	if _, err := ParseQueueMessage(noncanonical, fixture.messageID); err == nil {
		t.Fatal("ParseQueueMessage() accepted noncanonical JSON")
	}
}

type fixture struct {
	messageBody  []byte
	messageID    string
	manifestBody []byte
	batchBody    []byte
	manifestRef  BlobReference
	batchRef     BatchReference
	releaseID    string
	operationID  string
}

func newFixture(t *testing.T, recipients []Recipient) fixture {
	t.Helper()
	return newFixtureForExtension(t, recipients, "dar")
}

func newFixtureForExtension(t *testing.T, recipients []Recipient, extension string) fixture {
	t.Helper()
	if recipients == nil {
		recipients = []Recipient{{
			Email:     "ava.example@example.com",
			FirstName: "Ava",
			LastName:  "Example",
		}}
	}
	operationID := repeatHex("a", 64)
	releaseID := "dar_go_worker_test_01"
	batchBody := make([]byte, 0)
	for _, recipient := range recipients {
		row, err := json.Marshal(recipient)
		if err != nil {
			t.Fatal(err)
		}
		batchBody = append(batchBody, row...)
		batchBody = append(batchBody, '\n')
	}
	manifest := PublishedManifest{
		SchemaVersion:   "1.0",
		OperationID:     operationID,
		Repository:      "salehelnagar/dar-download",
		SourceCommitSHA: repeatHex("b", 40),
		ReleaseID:       releaseID,
		ReleaseVersion:  "1.0.0",
		DownloadName:    "go-worker-test-1.0.0." + extension,
		PublishedAt:     "2026-08-17T19:00:00Z",
		DAR: BlobReference{
			Container: "dar-releases",
			Name:      "dar/go-worker-test." + extension,
			VersionID: "dar-version-1",
			SHA256:    repeatHex("c", 64),
			Size:      100,
		},
	}
	manifestBody, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifestRef := reference("dar-release-manifests", "published/manifest.json", manifestBody)
	batchBlob := reference("dar-recipient-batches", "operation/batch-0000.jsonl", batchBody)
	batchRef := BatchReference{
		BatchCount:     1,
		BatchIndex:     0,
		Container:      batchBlob.Container,
		Name:           batchBlob.Name,
		RecipientCount: len(recipients),
		SHA256:         batchBlob.SHA256,
		Size:           batchBlob.Size,
		VersionID:      batchBlob.VersionID,
	}
	messageID := operationID + ":0"
	message := QueueMessage{
		SchemaVersion:   "1.0",
		MessageID:       messageID,
		OperationID:     operationID,
		ReleaseID:       releaseID,
		ReleaseVersion:  "1.0.0",
		SourceCommitSHA: repeatHex("b", 40),
		Manifest:        manifestRef,
		RecipientBatch:  batchRef,
		ApplicationURL:  "https://dar-poc.example.internal/v1/releases/1.0.0/download/go-worker-test-1.0.0." + extension,
		CreatedAt:       "2026-08-17T19:00:00Z",
	}
	messageBody, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	return fixture{
		messageBody: messageBody, messageID: messageID,
		manifestBody: manifestBody, batchBody: batchBody,
		manifestRef: manifestRef, batchRef: batchRef,
		releaseID: releaseID, operationID: operationID,
	}
}

func reference(container, name string, body []byte) BlobReference {
	digest := sha256.Sum256(body)
	return BlobReference{
		Container: container,
		Name:      name,
		VersionID: "version-1",
		SHA256:    hex.EncodeToString(digest[:]),
		Size:      int64(len(body)),
	}
}

func repeatHex(value string, count int) string {
	result := ""
	for len(result) < count {
		result += value
	}
	return result
}

func containsPII(body []byte) bool {
	for _, term := range [][]byte{[]byte("email"), []byte("first_name"), []byte("last_name"), []byte("@")} {
		if json.Valid(body) && stringContains(string(body), string(term)) {
			return true
		}
	}
	return false
}

func stringContains(value, term string) bool {
	for index := 0; index+len(term) <= len(value); index++ {
		if value[index:index+len(term)] == term {
			return true
		}
	}
	return false
}
