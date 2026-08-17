// Package distribution implements the durable DAR notification worker protocol.
package distribution

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/SalehElnagar/dar-download-app/internal/strictjson"
)

const (
	maxMessageBytes  = 64 * 1024
	maxManifestBytes = 64 * 1024
	maxArtifactBytes = int64(1024 * 1024 * 1024)
)

var (
	ErrContract         = errors.New("invalid distribution contract")
	ErrDependency       = errors.New("distribution dependency unavailable")
	digestPattern       = regexp.MustCompile(`^[a-f0-9]{64}$`)
	commitPattern       = regexp.MustCompile(`^[a-f0-9]{40}$`)
	releasePattern      = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9_-]{0,62}[a-z0-9])?$`)
	semverPattern       = regexp.MustCompile(`^(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-[0-9A-Za-z.-]+)?$`)
	repositoryPattern   = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	containerPattern    = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{1,61}[a-z0-9])?$`)
	downloadNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,122}\.(?:dar|zip)$`)
	namePattern         = regexp.MustCompile(`^[A-Za-z][A-Za-z .'-]{0,63}$`)
	emailPattern        = regexp.MustCompile(`^[a-z0-9.!#$%&'*+/=?^_` + "`" + `{|}~-]+@[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+$`)
)

// BlobReference binds one exact private Blob version and digest.
type BlobReference struct {
	Container string `json:"container"`
	Name      string `json:"name"`
	SHA256    string `json:"sha256"`
	Size      int64  `json:"size"`
	VersionID string `json:"version_id"`
}

// BatchReference adds bounded recipient-batch metadata to a Blob reference.
type BatchReference struct {
	BatchCount     int    `json:"batch_count"`
	BatchIndex     int    `json:"batch_index"`
	Container      string `json:"container"`
	Name           string `json:"name"`
	RecipientCount int    `json:"recipient_count"`
	SHA256         string `json:"sha256"`
	Size           int64  `json:"size"`
	VersionID      string `json:"version_id"`
}

// Reference returns the versioned Blob portion of a batch reference.
func (reference BatchReference) Reference() BlobReference {
	return BlobReference{
		Container: reference.Container,
		Name:      reference.Name,
		SHA256:    reference.SHA256,
		Size:      reference.Size,
		VersionID: reference.VersionID,
	}
}

// QueueMessage contains no recipient PII; it contains immutable references only.
type QueueMessage struct {
	ApplicationURL  string         `json:"application_url"`
	CreatedAt       string         `json:"created_at"`
	Manifest        BlobReference  `json:"manifest"`
	MessageID       string         `json:"message_id"`
	OperationID     string         `json:"operation_id"`
	RecipientBatch  BatchReference `json:"recipient_batch"`
	ReleaseID       string         `json:"release_id"`
	ReleaseVersion  string         `json:"release_version"`
	SchemaVersion   string         `json:"schema_version"`
	SourceCommitSHA string         `json:"source_commit_sha"`
}

// PublishedManifest binds the notification operation to one exact release artifact.
type PublishedManifest struct {
	DAR             BlobReference `json:"dar"`
	DownloadName    string        `json:"download_name"`
	OperationID     string        `json:"operation_id"`
	PublishedAt     string        `json:"published_at"`
	ReleaseID       string        `json:"release_id"`
	ReleaseVersion  string        `json:"release_version"`
	Repository      string        `json:"repository"`
	SchemaVersion   string        `json:"schema_version"`
	SourceCommitSHA string        `json:"source_commit_sha"`
}

// Recipient is one canonical row loaded only from a protected batch Blob.
type Recipient struct {
	Email     string `json:"email"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

// ParseQueueMessage rejects unknown fields, PII, noncanonical JSON, and changed broker identity.
func ParseQueueMessage(raw []byte, brokerMessageID string) (QueueMessage, error) {
	var message QueueMessage
	if err := strictjson.Decode(raw, maxMessageBytes, &message); err != nil {
		return QueueMessage{}, ErrContract
	}
	if !canonical(raw, message) || message.SchemaVersion != "1.0" ||
		message.MessageID != brokerMessageID ||
		message.MessageID != message.OperationID+":"+strconv.Itoa(message.RecipientBatch.BatchIndex) ||
		!digestPattern.MatchString(message.OperationID) ||
		!releasePattern.MatchString(message.ReleaseID) ||
		!semverPattern.MatchString(message.ReleaseVersion) ||
		!commitPattern.MatchString(message.SourceCommitSHA) ||
		!validTimestamp(message.CreatedAt) ||
		!validApplicationURL(message.ApplicationURL, message.ReleaseVersion) ||
		!message.Manifest.valid() || !message.RecipientBatch.valid() {
		return QueueMessage{}, ErrContract
	}
	return message, nil
}

// ParsePublishedManifest validates one canonical immutable manifest.
func ParsePublishedManifest(raw []byte) (PublishedManifest, error) {
	var manifest PublishedManifest
	if err := strictjson.Decode(raw, maxManifestBytes, &manifest); err != nil {
		return PublishedManifest{}, ErrContract
	}
	if !canonical(raw, manifest) || manifest.SchemaVersion != "1.0" ||
		!digestPattern.MatchString(manifest.OperationID) ||
		!repositoryPattern.MatchString(manifest.Repository) ||
		!commitPattern.MatchString(manifest.SourceCommitSHA) ||
		!releasePattern.MatchString(manifest.ReleaseID) ||
		!semverPattern.MatchString(manifest.ReleaseVersion) ||
		!downloadNamePattern.MatchString(manifest.DownloadName) ||
		!validTimestamp(manifest.PublishedAt) || !manifest.DAR.valid() {
		return PublishedManifest{}, ErrContract
	}
	return manifest, nil
}

// ParseRecipient validates one canonical JSONL row.
func ParseRecipient(raw []byte) (Recipient, error) {
	var recipient Recipient
	if err := strictjson.Decode(raw, 1024, &recipient); err != nil ||
		!canonical(raw, recipient) || !namePattern.MatchString(recipient.FirstName) ||
		!namePattern.MatchString(recipient.LastName) || len(recipient.Email) > 254 ||
		recipient.Email != strings.ToLower(strings.TrimSpace(recipient.Email)) ||
		!emailPattern.MatchString(recipient.Email) {
		return Recipient{}, ErrContract
	}
	return recipient, nil
}

// Matches checks both length and SHA-256 without trusting Blob metadata alone.
func (reference BlobReference) Matches(content []byte) bool {
	digest := sha256.Sum256(content)
	return int64(len(content)) == reference.Size && hex.EncodeToString(digest[:]) == reference.SHA256
}

func (reference BlobReference) valid() bool {
	return containerPattern.MatchString(reference.Container) && validBlobName(reference.Name) &&
		len(reference.VersionID) >= 1 && len(reference.VersionID) <= 128 &&
		controlFree(reference.VersionID) && digestPattern.MatchString(reference.SHA256) &&
		reference.Size >= 1 && reference.Size <= maxArtifactBytes
}

func (reference BatchReference) valid() bool {
	return reference.Reference().valid() && reference.BatchIndex >= 0 && reference.BatchIndex <= 9999 &&
		reference.BatchCount >= 1 && reference.BatchCount <= 10000 &&
		reference.BatchIndex < reference.BatchCount &&
		reference.RecipientCount >= 1 && reference.RecipientCount <= 500
}

func canonical(raw []byte, value any) bool {
	encoded, err := json.Marshal(value)
	return err == nil && bytes.Equal(raw, encoded)
}

func validTimestamp(value string) bool {
	if !strings.HasSuffix(value, "Z") {
		return false
	}
	parsed, err := time.Parse(time.RFC3339, value)
	return err == nil && parsed.Location() == time.UTC
}

func validApplicationURL(value, releaseVersion string) bool {
	parsed, err := url.Parse(value)
	if err != nil || parsed == nil {
		return false
	}
	pathParts := strings.Split(parsed.Path, "/")
	approvedPath := len(pathParts) == 6 && pathParts[0] == "" && pathParts[1] == "v1" &&
		pathParts[2] == "releases" && pathParts[3] == releaseVersion &&
		pathParts[4] == "download" && downloadNamePattern.MatchString(pathParts[5])
	if parsed.Scheme != "https" || parsed.User != nil || parsed.RawQuery != "" ||
		parsed.Fragment != "" || !approvedPath {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return (strings.HasSuffix(host, ".azurecontainerapps.io") || strings.HasSuffix(host, ".example.internal")) &&
		(parsed.Port() == "" || parsed.Port() == "443")
}

func validBlobName(value string) bool {
	if value == "" || len(value) > 1024 || strings.HasPrefix(value, "/") ||
		strings.HasSuffix(value, "/") || strings.ContainsAny(value, "\\?#") || !controlFree(value) {
		return false
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func controlFree(value string) bool {
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}
