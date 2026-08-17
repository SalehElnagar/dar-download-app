package distribution

import (
	"encoding/json"
	"errors"
	"regexp"
	"time"

	"github.com/SalehElnagar/dar-download-app/internal/strictjson"
)

// ReceiptStatus is one persisted notification state.
type ReceiptStatus string

const (
	ReceiptClaimed     ReceiptStatus = "CLAIMED"
	ReceiptSendStarted ReceiptStatus = "SEND_STARTED"
	ReceiptRetryable   ReceiptStatus = "RETRYABLE"
	ReceiptAccepted    ReceiptStatus = "ACCEPTED"
	ReceiptSimulated   ReceiptStatus = "SIMULATED"
	ReceiptFailed      ReceiptStatus = "FAILED"
	ReceiptUnknown     ReceiptStatus = "UNKNOWN"
)

var (
	ErrReceiptConflict  = errors.New("notification receipt conflict")
	versionLabelPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
	reasonPattern       = regexp.MustCompile(`^[A-Z0-9_]{1,64}$`)
)

// Receipt is the authoritative per-recipient effect journal.
// Field order intentionally matches canonical JSON key order.
type Receipt struct {
	AttemptCount       int           `json:"attempt_count"`
	ClaimedAt          *string       `json:"claimed_at"`
	HMACKeyVersion     string        `json:"hmac_key_version"`
	NextAttemptAt      *string       `json:"next_attempt_at"`
	OperationID        string        `json:"operation_id"`
	Provider           string        `json:"provider"`
	ProviderHTTPStatus *int          `json:"provider_http_status"`
	ProviderRequestID  *string       `json:"provider_request_id"`
	QueueMessageID     string        `json:"queue_message_id"`
	ReasonCode         *string       `json:"reason_code"`
	RecipientID        string        `json:"recipient_id"`
	ReleaseID          string        `json:"release_id"`
	ReleaseVersion     string        `json:"release_version"`
	SchemaVersion      string        `json:"schema_version"`
	SendStartedAt      *string       `json:"send_started_at"`
	StateVersion       int           `json:"state_version"`
	Status             ReceiptStatus `json:"status"`
	TerminalAt         *string       `json:"terminal_at"`
	UpdatedAt          string        `json:"updated_at"`
}

// ReceiptIntent binds a receipt to one immutable operation and HMAC identity.
type ReceiptIntent struct {
	OperationID    string
	ReleaseID      string
	ReleaseVersion string
	RecipientID    string
	HMACKeyVersion string
	Provider       string
	QueueMessageID string
}

// ClaimReceipt creates the first state without producing a mail effect.
func ClaimReceipt(intent ReceiptIntent, now time.Time) (Receipt, error) {
	if !digestPattern.MatchString(intent.OperationID) ||
		!releasePattern.MatchString(intent.ReleaseID) ||
		!semverPattern.MatchString(intent.ReleaseVersion) ||
		!digestPattern.MatchString(intent.RecipientID) ||
		!versionLabelPattern.MatchString(intent.HMACKeyVersion) ||
		(intent.Provider != "stub" && intent.Provider != "sendgrid") ||
		intent.QueueMessageID == "" || len(intent.QueueMessageID) > 128 || !controlFree(intent.QueueMessageID) ||
		now.Location() != time.UTC {
		return Receipt{}, ErrReceiptConflict
	}
	timestamp := formatTime(now)
	receipt := Receipt{
		AttemptCount: 1, ClaimedAt: &timestamp, HMACKeyVersion: intent.HMACKeyVersion,
		OperationID: intent.OperationID, Provider: intent.Provider,
		QueueMessageID: intent.QueueMessageID, RecipientID: intent.RecipientID,
		ReleaseID: intent.ReleaseID, ReleaseVersion: intent.ReleaseVersion,
		SchemaVersion: "1.0", StateVersion: 1, Status: ReceiptClaimed,
		UpdatedAt: timestamp,
	}
	return receipt, receipt.validate()
}

// ParseReceipt rejects unknown fields, duplicate keys, noncanonical JSON, and invalid state.
func ParseReceipt(raw []byte) (Receipt, error) {
	var receipt Receipt
	if err := strictjson.Decode(raw, 32*1024, &receipt); err != nil ||
		!canonical(raw, receipt) || receipt.validate() != nil {
		return Receipt{}, ErrReceiptConflict
	}
	return receipt, nil
}

// MarshalReceipt returns canonical bytes for Blob CAS persistence.
func MarshalReceipt(receipt Receipt) ([]byte, error) {
	if err := receipt.validate(); err != nil {
		return nil, err
	}
	return json.Marshal(receipt)
}

// StartSend commits the ambiguity boundary before calling a provider.
func (receipt Receipt) StartSend(now time.Time) (Receipt, error) {
	if receipt.Status != ReceiptClaimed || receipt.terminal() || now.Location() != time.UTC {
		return Receipt{}, ErrReceiptConflict
	}
	timestamp := formatTime(now)
	receipt.Status = ReceiptSendStarted
	receipt.StateVersion++
	receipt.SendStartedAt = &timestamp
	receipt.NextAttemptAt = nil
	receipt.ReasonCode = nil
	receipt.UpdatedAt = timestamp
	return receipt, receipt.validate()
}

// Accept records provider acceptance, which is not delivery proof.
func (receipt Receipt) Accept(now time.Time, status int, requestID string) (Receipt, error) {
	if status != 202 {
		return Receipt{}, ErrReceiptConflict
	}
	return receipt.finish(now, ReceiptAccepted, status, requestID, "")
}

// Simulate records stub or provider sandbox validation without delivery.
func (receipt Receipt) Simulate(now time.Time, status int, requestID string) (Receipt, error) {
	if status != 200 {
		return Receipt{}, ErrReceiptConflict
	}
	return receipt.finish(now, ReceiptSimulated, status, requestID, "")
}

// Retry records an explicit transient response and its next eligible time.
func (receipt Receipt) Retry(now time.Time, status int, reason string, retryAt time.Time) (Receipt, error) {
	if receipt.Status != ReceiptSendStarted || receipt.terminal() ||
		(status != 429 && (status < 500 || status > 599)) ||
		!reasonPattern.MatchString(reason) || retryAt.Before(now) ||
		now.Location() != time.UTC || retryAt.Location() != time.UTC {
		return Receipt{}, ErrReceiptConflict
	}
	next := formatTime(retryAt)
	receipt.Status = ReceiptRetryable
	receipt.StateVersion++
	receipt.ProviderHTTPStatus = &status
	receipt.ProviderRequestID = nil
	receipt.NextAttemptAt = &next
	receipt.ReasonCode = stringPointer(reason)
	receipt.UpdatedAt = formatTime(now)
	return receipt, receipt.validate()
}

// Fail records a known permanent outcome.
func (receipt Receipt) Fail(now time.Time, status int, reason string) (Receipt, error) {
	if status < 0 || status > 599 || !reasonPattern.MatchString(reason) {
		return Receipt{}, ErrReceiptConflict
	}
	return receipt.finish(now, ReceiptFailed, status, "", reason)
}

// MarkUnknown records a missing or ambiguous provider response and prevents blind resend.
func (receipt Receipt) MarkUnknown(now time.Time, reason string) (Receipt, error) {
	if !reasonPattern.MatchString(reason) {
		return Receipt{}, ErrReceiptConflict
	}
	return receipt.finish(now, ReceiptUnknown, 0, "", reason)
}

// Reclaim starts another bounded attempt only when the durable retry is due.
func (receipt Receipt) Reclaim(now time.Time, maxAttempts int) (Receipt, error) {
	if receipt.Status != ReceiptRetryable || receipt.terminal() || maxAttempts < 1 || maxAttempts > 5 ||
		receipt.AttemptCount >= maxAttempts || receipt.NextAttemptAt == nil {
		return Receipt{}, ErrReceiptConflict
	}
	next, err := parseTime(*receipt.NextAttemptAt)
	if err != nil || now.Before(next) || now.Location() != time.UTC {
		return Receipt{}, ErrReceiptConflict
	}
	timestamp := formatTime(now)
	receipt.Status = ReceiptClaimed
	receipt.StateVersion++
	receipt.AttemptCount++
	receipt.ClaimedAt = &timestamp
	receipt.SendStartedAt = nil
	receipt.NextAttemptAt = nil
	receipt.ProviderHTTPStatus = nil
	receipt.ProviderRequestID = nil
	receipt.ReasonCode = nil
	receipt.UpdatedAt = timestamp
	return receipt, receipt.validate()
}

// Exhaust closes a retryable receipt after its bounded attempt budget.
func (receipt Receipt) Exhaust(now time.Time) (Receipt, error) {
	if receipt.Status != ReceiptRetryable || receipt.terminal() {
		return Receipt{}, ErrReceiptConflict
	}
	receipt.Status = ReceiptFailed
	receipt.StateVersion++
	receipt.NextAttemptAt = nil
	receipt.ReasonCode = stringPointer("RETRY_BUDGET_EXHAUSTED")
	timestamp := formatTime(now)
	receipt.TerminalAt = &timestamp
	receipt.UpdatedAt = timestamp
	return receipt, receipt.validate()
}

// RecoverStale reclaims pre-send work or makes a stale send permanently UNKNOWN.
func (receipt Receipt) RecoverStale(now time.Time, timeout time.Duration, maxAttempts int) (Receipt, error) {
	if receipt.terminal() || timeout <= 0 || now.Location() != time.UTC {
		return Receipt{}, ErrReceiptConflict
	}
	switch receipt.Status {
	case ReceiptClaimed:
		if receipt.ClaimedAt == nil || receipt.AttemptCount >= maxAttempts {
			return Receipt{}, ErrReceiptConflict
		}
		claimed, err := parseTime(*receipt.ClaimedAt)
		if err != nil || now.Sub(claimed) < timeout {
			return Receipt{}, ErrReceiptConflict
		}
		timestamp := formatTime(now)
		receipt.AttemptCount++
		receipt.StateVersion++
		receipt.ClaimedAt = &timestamp
		receipt.UpdatedAt = timestamp
		return receipt, receipt.validate()
	case ReceiptSendStarted:
		if receipt.SendStartedAt == nil {
			return Receipt{}, ErrReceiptConflict
		}
		started, err := parseTime(*receipt.SendStartedAt)
		if err != nil || now.Sub(started) < timeout {
			return Receipt{}, ErrReceiptConflict
		}
		return receipt.MarkUnknown(now, "STALE_SEND_OUTCOME")
	default:
		return Receipt{}, ErrReceiptConflict
	}
}

func (receipt Receipt) finish(now time.Time, status ReceiptStatus, httpStatus int, requestID, reason string) (Receipt, error) {
	if receipt.Status != ReceiptSendStarted || receipt.terminal() || now.Location() != time.UTC {
		return Receipt{}, ErrReceiptConflict
	}
	receipt.Status = status
	receipt.StateVersion++
	if httpStatus != 0 {
		receipt.ProviderHTTPStatus = &httpStatus
	} else {
		receipt.ProviderHTTPStatus = nil
	}
	if requestID != "" {
		if len(requestID) > 128 || !controlFree(requestID) {
			return Receipt{}, ErrReceiptConflict
		}
		receipt.ProviderRequestID = &requestID
	} else {
		receipt.ProviderRequestID = nil
	}
	if reason != "" {
		receipt.ReasonCode = stringPointer(reason)
	} else {
		receipt.ReasonCode = nil
	}
	timestamp := formatTime(now)
	receipt.TerminalAt = &timestamp
	receipt.NextAttemptAt = nil
	receipt.UpdatedAt = timestamp
	return receipt, receipt.validate()
}

func (receipt Receipt) validate() error {
	if receipt.SchemaVersion != "1.0" || !digestPattern.MatchString(receipt.OperationID) ||
		!releasePattern.MatchString(receipt.ReleaseID) || !semverPattern.MatchString(receipt.ReleaseVersion) ||
		!digestPattern.MatchString(receipt.RecipientID) || !versionLabelPattern.MatchString(receipt.HMACKeyVersion) ||
		(receipt.Provider != "stub" && receipt.Provider != "sendgrid") ||
		receipt.AttemptCount < 1 || receipt.AttemptCount > 5 || receipt.StateVersion < 1 ||
		receipt.QueueMessageID == "" || len(receipt.QueueMessageID) > 128 ||
		!controlFree(receipt.QueueMessageID) || !validReceiptStatus(receipt.Status) ||
		!validReceiptTime(receipt.UpdatedAt) ||
		(receipt.ReasonCode != nil && !reasonPattern.MatchString(*receipt.ReasonCode)) {
		return ErrReceiptConflict
	}
	for _, value := range []*string{receipt.ClaimedAt, receipt.NextAttemptAt, receipt.SendStartedAt, receipt.TerminalAt} {
		if value != nil && !validReceiptTime(*value) {
			return ErrReceiptConflict
		}
	}
	if receipt.ProviderRequestID != nil && (len(*receipt.ProviderRequestID) > 128 || !controlFree(*receipt.ProviderRequestID)) {
		return ErrReceiptConflict
	}
	return nil
}

func (receipt Receipt) terminal() bool {
	return receipt.Status == ReceiptAccepted || receipt.Status == ReceiptSimulated ||
		receipt.Status == ReceiptFailed || receipt.Status == ReceiptUnknown
}

func validReceiptStatus(status ReceiptStatus) bool {
	switch status {
	case ReceiptClaimed, ReceiptSendStarted, ReceiptRetryable, ReceiptAccepted,
		ReceiptSimulated, ReceiptFailed, ReceiptUnknown:
		return true
	default:
		return false
	}
}

func validReceiptTime(value string) bool {
	_, err := parseTime(value)
	return err == nil
}

func parseTime(value string) (time.Time, error) {
	parsed, err := time.Parse("2006-01-02T15:04:05Z", value)
	if err != nil || parsed.Location() != time.UTC {
		return time.Time{}, ErrReceiptConflict
	}
	return parsed, nil
}

func formatTime(value time.Time) string {
	return value.UTC().Format("2006-01-02T15:04:05Z")
}

func stringPointer(value string) *string {
	return &value
}
