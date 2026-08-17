package distribution

import (
	"bufio"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"math"
	"time"
)

// Disposition tells the Service Bus adapter how to settle one delivery.
type Disposition string

const (
	Complete   Disposition = "COMPLETE"
	Abandon    Disposition = "ABANDON"
	DeadLetter Disposition = "DEAD_LETTER"
)

// ProcessResult contains only bounded, PII-free settlement evidence.
type ProcessResult struct {
	Disposition    Disposition
	ReasonCode     string
	ProcessedCount int
	SkippedCount   int
	RetryAfter     time.Duration
}

// StoredReceipt binds one receipt to the ETag required for its next CAS.
type StoredReceipt struct {
	Receipt Receipt
	ETag    string
}

// BlobStore exposes only exact-version verified reads.
type BlobStore interface {
	ReadSmall(context.Context, BlobReference, int64) ([]byte, error)
	Verify(context.Context, BlobReference) error
	Open(context.Context, BlobReference) (io.ReadCloser, error)
}

// ReceiptStore owns conditional receipt creates and replacements.
type ReceiptStore interface {
	Get(context.Context, string) (StoredReceipt, bool, error)
	Create(context.Context, string, Receipt) (StoredReceipt, error)
	Replace(context.Context, string, string, Receipt) (StoredReceipt, error)
}

// MailOutcome classifies only authoritative provider observations.
type MailOutcome string

const (
	MailAccepted  MailOutcome = "ACCEPTED"
	MailSimulated MailOutcome = "SIMULATED"
	MailRetryable MailOutcome = "RETRYABLE"
	MailPermanent MailOutcome = "PERMANENT_FAILURE"
	MailUnknown   MailOutcome = "UNKNOWN"
)

// MailResult never contains a response body or recipient PII.
type MailResult struct {
	Outcome    MailOutcome
	HTTPStatus int
	RequestID  string
	ReasonCode string
	RetryAfter time.Duration
}

// Notification is the one-row transient mail input. It is never persisted or logged.
type Notification struct {
	OperationID    string
	ReleaseID      string
	ReleaseVersion string
	RecipientID    string
	FirstName      string
	LastName       string
	Email          string
	ApplicationURL string
}

// Mailer performs the only external notification effect.
type Mailer interface {
	Send(context.Context, Notification) MailResult
}

// AuditEvent is structured and deliberately omits names, email, headers, and bodies.
type AuditEvent struct {
	EventName           string
	OccurredAt          string
	OperationID         string
	ReleaseID           string
	ReleaseVersion      string
	RecipientID         string
	HMACKeyVersion      string
	QueueMessageID      string
	Provider            string
	ReceiptStatus       ReceiptStatus
	ReceiptStateVersion int
	AttemptCount        int
	ProviderHTTPStatus  int
	ReasonCode          string
}

// Auditor receives unsampled committed state transitions.
type Auditor interface {
	Emit(AuditEvent)
}

type discardAudit struct{}

func (discardAudit) Emit(AuditEvent) {}

// WorkerOptions are immutable and contain no ambient credentials.
type WorkerOptions struct {
	Blobs          BlobStore
	Receipts       ReceiptStore
	Mailer         Mailer
	Auditor        Auditor
	HMACKey        []byte
	HMACKeyVersion string
	Provider       string
	Clock          func() time.Time
	MaxAttempts    int
	ClaimTimeout   time.Duration
}

// Worker applies the receipt state machine before and after every mail effect.
type Worker struct {
	options WorkerOptions
}

// NewWorker rejects an unsafe or unbounded runtime policy.
func NewWorker(options WorkerOptions) *Worker {
	if options.Blobs == nil || options.Receipts == nil || options.Mailer == nil ||
		len(options.HMACKey) < 32 || len(options.HMACKey) > 64 ||
		!versionLabelPattern.MatchString(options.HMACKeyVersion) ||
		(options.Provider != "stub" && options.Provider != "sendgrid") ||
		options.Clock == nil || options.MaxAttempts < 1 || options.MaxAttempts > 5 ||
		options.ClaimTimeout <= 0 {
		panic("invalid distribution worker options")
	}
	options.HMACKey = append([]byte(nil), options.HMACKey...)
	if options.Auditor == nil {
		options.Auditor = discardAudit{}
	}
	return &Worker{options: options}
}

// Process validates every immutable reference and every row before the first mail effect.
func (worker *Worker) Process(ctx context.Context, brokerMessageID string, body []byte) ProcessResult {
	message, err := ParseQueueMessage(body, brokerMessageID)
	if err != nil {
		return ProcessResult{Disposition: DeadLetter, ReasonCode: "MESSAGE_INVALID"}
	}
	manifestBody, err := worker.options.Blobs.ReadSmall(ctx, message.Manifest, maxManifestBytes)
	if err != nil {
		if errors.Is(err, ErrDependency) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return ProcessResult{Disposition: Abandon, ReasonCode: "BLOB_UNAVAILABLE", RetryAfter: time.Second}
		}
		return ProcessResult{Disposition: DeadLetter, ReasonCode: "REFERENCED_BLOB_INVALID"}
	}
	manifest, err := ParsePublishedManifest(manifestBody)
	if err != nil || !manifestMatchesMessage(manifest, message) {
		return ProcessResult{Disposition: DeadLetter, ReasonCode: "REFERENCED_BLOB_INVALID"}
	}
	batchReference := message.RecipientBatch.Reference()
	if err := worker.options.Blobs.Verify(ctx, batchReference); err != nil {
		if errors.Is(err, ErrDependency) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return ProcessResult{Disposition: Abandon, ReasonCode: "BLOB_UNAVAILABLE", RetryAfter: time.Second}
		}
		return ProcessResult{Disposition: DeadLetter, ReasonCode: "REFERENCED_BLOB_INVALID"}
	}
	if err := worker.validateBatch(ctx, batchReference, message.RecipientBatch.RecipientCount); err != nil {
		return ProcessResult{Disposition: DeadLetter, ReasonCode: "REFERENCED_BLOB_INVALID"}
	}

	reader, err := worker.options.Blobs.Open(ctx, batchReference)
	if err != nil {
		return ProcessResult{Disposition: Abandon, ReasonCode: "BLOB_UNAVAILABLE", RetryAfter: time.Second}
	}
	defer reader.Close()
	result := ProcessResult{Disposition: Complete}
	err = scanRecipientRows(reader, func(recipient Recipient) error {
		recipientResult := worker.processRecipient(ctx, message, recipient)
		if recipientResult == nil {
			result.SkippedCount++
			return nil
		}
		result.ProcessedCount++
		if recipientResult.Disposition != Complete {
			result.Disposition = recipientResult.Disposition
			result.ReasonCode = recipientResult.ReasonCode
			result.RetryAfter = recipientResult.RetryAfter
			return errStopBatch
		}
		return nil
	})
	if errors.Is(err, errStopBatch) {
		return result
	}
	if err != nil {
		return ProcessResult{Disposition: DeadLetter, ReasonCode: "REFERENCED_BLOB_INVALID"}
	}
	return result
}

var errStopBatch = errors.New("stop batch")

func (worker *Worker) validateBatch(ctx context.Context, reference BlobReference, expectedCount int) error {
	reader, err := worker.options.Blobs.Open(ctx, reference)
	if err != nil {
		return err
	}
	defer reader.Close()
	count := 0
	err = scanRecipientRows(reader, func(Recipient) error {
		count++
		return nil
	})
	if err != nil || count != expectedCount {
		return ErrContract
	}
	return nil
}

func (worker *Worker) processRecipient(ctx context.Context, message QueueMessage, recipient Recipient) *ProcessResult {
	now := worker.options.Clock().UTC()
	recipientMAC := hmac.New(sha256.New, worker.options.HMACKey)
	_, _ = recipientMAC.Write([]byte(recipient.Email))
	recipientID := hex.EncodeToString(recipientMAC.Sum(nil))
	path := message.ReleaseID + "/" + message.OperationID + "/" +
		worker.options.HMACKeyVersion + "/" + recipientID + ".json"
	stored, exists, err := worker.options.Receipts.Get(ctx, path)
	ownedClaim := false
	if err != nil {
		return &ProcessResult{Disposition: Abandon, ReasonCode: "RECEIPT_UNAVAILABLE", RetryAfter: time.Second}
	}
	if !exists {
		claimed, claimErr := ClaimReceipt(ReceiptIntent{
			OperationID: message.OperationID, ReleaseID: message.ReleaseID,
			ReleaseVersion: message.ReleaseVersion, RecipientID: recipientID,
			HMACKeyVersion: worker.options.HMACKeyVersion, Provider: worker.options.Provider,
			QueueMessageID: message.MessageID,
		}, now)
		if claimErr != nil {
			return &ProcessResult{Disposition: DeadLetter, ReasonCode: "RECEIPT_CONFLICT"}
		}
		stored, err = worker.options.Receipts.Create(ctx, path, claimed)
		if err == nil {
			ownedClaim = true
		} else if errors.Is(err, ErrReceiptConflict) {
			stored, exists, err = worker.options.Receipts.Get(ctx, path)
			if err != nil || !exists {
				return &ProcessResult{Disposition: Abandon, ReasonCode: "RECEIPT_CONFLICT", RetryAfter: time.Second}
			}
		} else {
			return &ProcessResult{Disposition: Abandon, ReasonCode: "RECEIPT_UNAVAILABLE", RetryAfter: time.Second}
		}
	}
	if !receiptMatches(stored.Receipt, message, recipientID) {
		return &ProcessResult{Disposition: DeadLetter, ReasonCode: "RECEIPT_CONFLICT"}
	}
	switch stored.Receipt.Status {
	case ReceiptAccepted, ReceiptSimulated:
		return nil
	case ReceiptFailed, ReceiptUnknown:
		return &ProcessResult{Disposition: DeadLetter, ReasonCode: receiptReason(stored.Receipt, "TERMINAL_NOTIFICATION_FAILURE")}
	case ReceiptRetryable:
		if stored.Receipt.AttemptCount >= worker.options.MaxAttempts {
			exhausted, exhaustErr := stored.Receipt.Exhaust(now)
			if exhaustErr != nil {
				return &ProcessResult{Disposition: DeadLetter, ReasonCode: "RECEIPT_CONFLICT"}
			}
			stored, err = worker.options.Receipts.Replace(ctx, path, stored.ETag, exhausted)
			if err != nil {
				return &ProcessResult{Disposition: Abandon, ReasonCode: "RECEIPT_CONFLICT", RetryAfter: time.Second}
			}
			worker.audit("dar.notification.terminal", stored.Receipt)
			return &ProcessResult{Disposition: DeadLetter, ReasonCode: "RETRY_BUDGET_EXHAUSTED"}
		}
		if stored.Receipt.NextAttemptAt != nil {
			next, parseErr := parseTime(*stored.Receipt.NextAttemptAt)
			if parseErr != nil {
				return &ProcessResult{Disposition: DeadLetter, ReasonCode: "RECEIPT_CONFLICT"}
			}
			if now.Before(next) {
				return &ProcessResult{Disposition: Abandon, ReasonCode: "RETRY_NOT_DUE", RetryAfter: boundedDelay(next.Sub(now))}
			}
		}
		reclaimed, reclaimErr := stored.Receipt.Reclaim(now, worker.options.MaxAttempts)
		if reclaimErr != nil {
			return &ProcessResult{Disposition: Abandon, ReasonCode: "RECEIPT_CONFLICT", RetryAfter: time.Second}
		}
		stored, err = worker.options.Receipts.Replace(ctx, path, stored.ETag, reclaimed)
		if err != nil {
			return &ProcessResult{Disposition: Abandon, ReasonCode: "RECEIPT_CONFLICT", RetryAfter: time.Second}
		}
		ownedClaim = true
	case ReceiptClaimed, ReceiptSendStarted:
		if !ownedClaim {
			recovered, recoverErr := stored.Receipt.RecoverStale(
				now, worker.options.ClaimTimeout, worker.options.MaxAttempts,
			)
			if recoverErr != nil {
				return &ProcessResult{Disposition: Abandon, ReasonCode: "ACTIVE_RECEIPT", RetryAfter: worker.activeDelay(stored.Receipt, now)}
			}
			stored, err = worker.options.Receipts.Replace(ctx, path, stored.ETag, recovered)
			if err != nil {
				return &ProcessResult{Disposition: Abandon, ReasonCode: "RECEIPT_CONFLICT", RetryAfter: time.Second}
			}
			if stored.Receipt.Status == ReceiptUnknown {
				worker.audit("dar.notification.terminal", stored.Receipt)
				return &ProcessResult{Disposition: DeadLetter, ReasonCode: receiptReason(stored.Receipt, "STALE_SEND_OUTCOME")}
			}
		}
	default:
		return &ProcessResult{Disposition: DeadLetter, ReasonCode: "RECEIPT_CONFLICT"}
	}

	started, err := stored.Receipt.StartSend(now)
	if err != nil {
		return &ProcessResult{Disposition: Abandon, ReasonCode: "RECEIPT_CONFLICT", RetryAfter: time.Second}
	}
	stored, err = worker.options.Receipts.Replace(ctx, path, stored.ETag, started)
	if err != nil {
		return &ProcessResult{Disposition: Abandon, ReasonCode: "RECEIPT_CONFLICT", RetryAfter: time.Second}
	}
	worker.audit("dar.notification.send_started", stored.Receipt)
	mailResult := worker.options.Mailer.Send(ctx, Notification{
		OperationID: message.OperationID, ReleaseID: message.ReleaseID,
		ReleaseVersion: message.ReleaseVersion, RecipientID: recipientID,
		FirstName: recipient.FirstName, LastName: recipient.LastName, Email: recipient.Email,
		ApplicationURL: message.ApplicationURL,
	})
	updated, disposition, reason, retryAfter := applyMailResult(stored.Receipt, mailResult, now)
	if updated.Status == "" {
		return &ProcessResult{Disposition: DeadLetter, ReasonCode: "PROVIDER_PROTOCOL_UNKNOWN"}
	}
	stored, err = worker.options.Receipts.Replace(ctx, path, stored.ETag, updated)
	if err != nil {
		return &ProcessResult{Disposition: DeadLetter, ReasonCode: "RECEIPT_COMMIT_UNKNOWN"}
	}
	eventName := "dar.notification.terminal"
	if stored.Receipt.Status == ReceiptRetryable {
		eventName = "dar.notification.retry_scheduled"
	}
	worker.audit(eventName, stored.Receipt)
	return &ProcessResult{Disposition: disposition, ReasonCode: reason, RetryAfter: retryAfter}
}

func applyMailResult(receipt Receipt, result MailResult, now time.Time) (Receipt, Disposition, string, time.Duration) {
	var updated Receipt
	var err error
	switch result.Outcome {
	case MailAccepted:
		updated, err = receipt.Accept(now, result.HTTPStatus, result.RequestID)
		return updatedOrZero(updated, err), Complete, "", 0
	case MailSimulated:
		updated, err = receipt.Simulate(now, result.HTTPStatus, result.RequestID)
		return updatedOrZero(updated, err), Complete, "", 0
	case MailRetryable:
		delay := retryDelay(receipt, result)
		reason := result.ReasonCode
		if reason == "" {
			reason = "PROVIDER_RETRYABLE"
		}
		updated, err = receipt.Retry(now, result.HTTPStatus, reason, now.Add(delay))
		return updatedOrZero(updated, err), Abandon, reason, delay
	case MailPermanent:
		reason := result.ReasonCode
		if reason == "" {
			reason = "PROVIDER_REJECTED"
		}
		updated, err = receipt.Fail(now, result.HTTPStatus, reason)
		return updatedOrZero(updated, err), DeadLetter, reason, 0
	default:
		reason := result.ReasonCode
		if reason == "" {
			reason = "PROVIDER_OUTCOME_UNKNOWN"
		}
		updated, err = receipt.MarkUnknown(now, reason)
		return updatedOrZero(updated, err), DeadLetter, reason, 0
	}
}

func updatedOrZero(receipt Receipt, err error) Receipt {
	if err != nil {
		return Receipt{}
	}
	return receipt
}

func retryDelay(receipt Receipt, result MailResult) time.Duration {
	exponential := time.Second * time.Duration(1<<max(receipt.AttemptCount-1, 0))
	base := max(result.RetryAfter, exponential)
	if base < time.Second {
		base = time.Second
	}
	if base > 300*time.Second {
		base = 300 * time.Second
	}
	jitterRange := min(max(base/4, time.Second), 10*time.Second)
	jitterSlots := int64(jitterRange/time.Second) + 1
	jitterSeed := int64(0)
	for _, character := range []byte(receipt.RecipientID[:8]) {
		jitterSeed = (jitterSeed*33 + int64(character)) & math.MaxInt32
	}
	delay := base + time.Duration(jitterSeed%jitterSlots)*time.Second
	return boundedDelay(delay)
}

func boundedDelay(value time.Duration) time.Duration {
	if value < time.Second {
		return time.Second
	}
	if value > 300*time.Second {
		return 300 * time.Second
	}
	return value
}

func (worker *Worker) activeDelay(receipt Receipt, now time.Time) time.Duration {
	anchor := receipt.ClaimedAt
	if receipt.Status == ReceiptSendStarted {
		anchor = receipt.SendStartedAt
	}
	if anchor == nil {
		return time.Second
	}
	started, err := parseTime(*anchor)
	if err != nil {
		return time.Second
	}
	return boundedDelay(worker.options.ClaimTimeout - now.Sub(started))
}

func (worker *Worker) audit(eventName string, receipt Receipt) {
	reason := ""
	if receipt.ReasonCode != nil {
		reason = *receipt.ReasonCode
	}
	httpStatus := 0
	if receipt.ProviderHTTPStatus != nil {
		httpStatus = *receipt.ProviderHTTPStatus
	}
	worker.options.Auditor.Emit(AuditEvent{
		EventName: eventName, OccurredAt: receipt.UpdatedAt,
		OperationID: receipt.OperationID, ReleaseID: receipt.ReleaseID,
		ReleaseVersion: receipt.ReleaseVersion, RecipientID: receipt.RecipientID,
		HMACKeyVersion: receipt.HMACKeyVersion, QueueMessageID: receipt.QueueMessageID,
		Provider: receipt.Provider, ReceiptStatus: receipt.Status,
		ReceiptStateVersion: receipt.StateVersion, AttemptCount: receipt.AttemptCount,
		ProviderHTTPStatus: httpStatus, ReasonCode: reason,
	})
}

func manifestMatchesMessage(manifest PublishedManifest, message QueueMessage) bool {
	return manifest.OperationID == message.OperationID && manifest.ReleaseID == message.ReleaseID &&
		manifest.ReleaseVersion == message.ReleaseVersion && manifest.SourceCommitSHA == message.SourceCommitSHA
}

func receiptMatches(receipt Receipt, message QueueMessage, recipientID string) bool {
	return receipt.OperationID == message.OperationID && receipt.ReleaseID == message.ReleaseID &&
		receipt.ReleaseVersion == message.ReleaseVersion && receipt.RecipientID == recipientID &&
		receipt.QueueMessageID == message.MessageID && receipt.HMACKeyVersion != ""
}

func receiptReason(receipt Receipt, fallback string) string {
	if receipt.ReasonCode != nil {
		return *receipt.ReasonCode
	}
	return fallback
}

func scanRecipientRows(reader io.Reader, process func(Recipient) error) error {
	tracking := &lastByteReader{reader: reader}
	scanner := bufio.NewScanner(tracking)
	scanner.Buffer(make([]byte, 1024), 1024)
	count := 0
	for scanner.Scan() {
		raw := scanner.Bytes()
		if len(raw) == 0 {
			return ErrContract
		}
		recipient, err := ParseRecipient(raw)
		if err != nil {
			return err
		}
		count++
		if err := process(recipient); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil || count == 0 || tracking.last != '\n' {
		return ErrContract
	}
	return nil
}

type lastByteReader struct {
	reader io.Reader
	last   byte
}

func (reader *lastByteReader) Read(buffer []byte) (int, error) {
	count, err := reader.reader.Read(buffer)
	if count > 0 {
		reader.last = buffer[count-1]
	}
	return count, err
}
