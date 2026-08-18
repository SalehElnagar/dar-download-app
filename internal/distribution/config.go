package distribution

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	appconfig "github.com/SalehElnagar/dar-download-app/internal/config"
)

const (
	WorkerIdentityClientIDEnv    = "DAR_WORKER_IDENTITY_CLIENT_ID"
	WorkerStorageAccountEnv      = "DAR_STORAGE_ACCOUNT_NAME"
	WorkerServiceBusNamespaceEnv = "DAR_SERVICEBUS_NAMESPACE"
	WorkerServiceBusQueueEnv     = "DAR_SERVICEBUS_QUEUE"
	WorkerManifestsContainerEnv  = "DAR_MANIFESTS_CONTAINER"
	WorkerBatchesContainerEnv    = "DAR_BATCHES_CONTAINER"
	WorkerReceiptsContainerEnv   = "DAR_RECEIPTS_CONTAINER"
	WorkerHMACKeyEnv             = "DAR_RECEIPT_HMAC_KEY_B64"
	WorkerHMACKeyVersionEnv      = "DAR_RECEIPT_HMAC_KEY_VERSION"
	WorkerMailModeEnv            = "DAR_MAIL_MODE"
	WorkerMailFromEmailEnv       = "DAR_MAIL_FROM_EMAIL"
	WorkerMailFromNameEnv        = "DAR_MAIL_FROM_NAME"
	WorkerAllowedRecipientsEnv   = "DAR_MAIL_ALLOWED_RECIPIENTS_JSON"
	WorkerMailAPIKeyEnv          = "DAR_MAIL_API_KEY"
	WorkerStubEndpointEnv        = "DAR_STUB_ENDPOINT"
	WorkerMaxAttemptsEnv         = "DAR_MAX_ATTEMPTS"
	WorkerClaimTimeoutSecondsEnv = "DAR_CLAIM_TIMEOUT_SECONDS"
	WorkerMailTimeoutSecondsEnv  = "DAR_MAIL_TIMEOUT_SECONDS"
	WorkerHealthPortEnv          = "DAR_HEALTH_PORT"
)

var (
	ErrWorkerConfig            = errors.New("invalid distribution worker configuration")
	storageAccountPattern      = regexp.MustCompile(`^[a-z0-9]{3,24}$`)
	serviceBusNamespacePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{4,48}[a-z0-9]\.servicebus\.windows\.net$`)
	queuePattern               = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,258}[A-Za-z0-9]$`)
)

// RuntimeConfig is the complete no-ingress worker policy.
type RuntimeConfig struct {
	IdentityClientID    string
	StorageAccount      string
	ServiceBusNamespace string
	ServiceBusQueue     string
	ManifestsContainer  string
	BatchesContainer    string
	ReceiptsContainer   string
	HMACKeyVersion      string
	MailMode            MailMode
	MailFromEmail       string
	MailFromName        string
	MaxAttempts         int
	ClaimTimeout        time.Duration
	MailTimeout         time.Duration
	HealthPort          int
	hmacKey             []byte
	allowedRecipients   []string
	mailAPIKey          string
	stubEndpoint        string
}

// String deliberately omits HMAC, recipient, and provider credential values.
func (config RuntimeConfig) String() string {
	return fmt.Sprintf(
		"RuntimeConfig{IdentityClientID:%s StorageAccount:%s ServiceBusNamespace:%s ServiceBusQueue:%s MailMode:%s HMACKeyVersion:%s}",
		config.IdentityClientID, config.StorageAccount, config.ServiceBusNamespace,
		config.ServiceBusQueue, config.MailMode, config.HMACKeyVersion,
	)
}

// HMACKey returns a defensive copy for the worker core.
func (config RuntimeConfig) HMACKey() []byte {
	return append([]byte(nil), config.hmacKey...)
}

// Provider returns the receipt-safe provider label.
func (config RuntimeConfig) Provider() string {
	if config.MailMode == MailModeStub {
		return "stub"
	}
	return "sendgrid"
}

// MailerConfig returns the validated effect boundary policy.
func (config RuntimeConfig) MailerConfig() MailConfig {
	return MailConfig{
		Mode: config.MailMode, FromEmail: config.MailFromEmail, FromName: config.MailFromName,
		AllowedRecipients: append([]string(nil), config.allowedRecipients...),
		APIKey:            config.mailAPIKey, StubEndpoint: config.stubEndpoint, Timeout: config.MailTimeout,
	}
}

// LoadWorkerEnvironment reads only the documented worker variables.
func LoadWorkerEnvironment() (RuntimeConfig, error) {
	environment := make(map[string]string)
	for _, name := range []string{
		WorkerIdentityClientIDEnv, WorkerStorageAccountEnv, WorkerServiceBusNamespaceEnv,
		WorkerServiceBusQueueEnv, WorkerManifestsContainerEnv, WorkerBatchesContainerEnv,
		WorkerReceiptsContainerEnv, WorkerHMACKeyEnv, WorkerHMACKeyVersionEnv,
		WorkerMailModeEnv, WorkerMailFromEmailEnv, WorkerMailFromNameEnv,
		WorkerAllowedRecipientsEnv, WorkerMailAPIKeyEnv, WorkerStubEndpointEnv,
		WorkerMaxAttemptsEnv, WorkerClaimTimeoutSecondsEnv, WorkerMailTimeoutSecondsEnv,
		WorkerHealthPortEnv,
	} {
		if value, exists := os.LookupEnv(name); exists {
			environment[name] = value
		}
	}
	return ParseWorkerEnvironment(environment)
}

// ParseWorkerEnvironment fails closed before constructing Azure or mail clients.
func ParseWorkerEnvironment(environment map[string]string) (RuntimeConfig, error) {
	required := []string{
		WorkerIdentityClientIDEnv, WorkerStorageAccountEnv, WorkerServiceBusNamespaceEnv,
		WorkerServiceBusQueueEnv, WorkerManifestsContainerEnv, WorkerBatchesContainerEnv,
		WorkerReceiptsContainerEnv, WorkerHMACKeyEnv, WorkerHMACKeyVersionEnv,
		WorkerMailModeEnv, WorkerMailFromEmailEnv, WorkerMailFromNameEnv,
		WorkerAllowedRecipientsEnv,
	}
	for _, name := range required {
		if strings.TrimSpace(environment[name]) == "" {
			return RuntimeConfig{}, ErrWorkerConfig
		}
	}
	identity := environment[WorkerIdentityClientIDEnv]
	storageAccount := environment[WorkerStorageAccountEnv]
	namespace := strings.ToLower(environment[WorkerServiceBusNamespaceEnv])
	queue := environment[WorkerServiceBusQueueEnv]
	containers := []string{
		environment[WorkerManifestsContainerEnv],
		environment[WorkerBatchesContainerEnv],
		environment[WorkerReceiptsContainerEnv],
	}
	if !appconfig.IsCanonicalUUID(identity) || !storageAccountPattern.MatchString(storageAccount) ||
		!serviceBusNamespacePattern.MatchString(namespace) || !queuePattern.MatchString(queue) ||
		strings.Contains(queue, "//") || strings.Contains(queue, "..") ||
		!validContainer(containers[0]) || !validContainer(containers[1]) || !validContainer(containers[2]) ||
		containers[0] == containers[1] || containers[0] == containers[2] || containers[1] == containers[2] ||
		!versionLabelPattern.MatchString(environment[WorkerHMACKeyVersionEnv]) {
		return RuntimeConfig{}, ErrWorkerConfig
	}
	hmacKey, err := base64.StdEncoding.Strict().DecodeString(environment[WorkerHMACKeyEnv])
	if err != nil || len(hmacKey) < 32 || len(hmacKey) > 64 ||
		base64.StdEncoding.EncodeToString(hmacKey) != environment[WorkerHMACKeyEnv] {
		return RuntimeConfig{}, ErrWorkerConfig
	}
	var recipients []string
	if err := json.Unmarshal([]byte(environment[WorkerAllowedRecipientsEnv]), &recipients); err != nil ||
		len(recipients) == 0 {
		return RuntimeConfig{}, ErrWorkerConfig
	}
	maxAttempts, err := boundedInteger(environment[WorkerMaxAttemptsEnv], 5, 1, 5)
	if err != nil {
		return RuntimeConfig{}, err
	}
	claimSeconds, err := boundedInteger(environment[WorkerClaimTimeoutSecondsEnv], 300, 30, 900)
	if err != nil {
		return RuntimeConfig{}, err
	}
	mailSeconds, err := boundedInteger(environment[WorkerMailTimeoutSecondsEnv], 10, 1, 30)
	if err != nil {
		return RuntimeConfig{}, err
	}
	healthPort, err := boundedInteger(environment[WorkerHealthPortEnv], 8081, 1024, 65535)
	if err != nil {
		return RuntimeConfig{}, err
	}
	config := RuntimeConfig{
		IdentityClientID: identity, StorageAccount: storageAccount,
		ServiceBusNamespace: namespace, ServiceBusQueue: queue,
		ManifestsContainer: containers[0], BatchesContainer: containers[1], ReceiptsContainer: containers[2],
		HMACKeyVersion: environment[WorkerHMACKeyVersionEnv], MailMode: MailMode(environment[WorkerMailModeEnv]),
		MailFromEmail: environment[WorkerMailFromEmailEnv], MailFromName: environment[WorkerMailFromNameEnv],
		MaxAttempts: maxAttempts, ClaimTimeout: time.Duration(claimSeconds) * time.Second,
		MailTimeout: time.Duration(mailSeconds) * time.Second, HealthPort: healthPort,
		hmacKey: hmacKey, allowedRecipients: append([]string(nil), recipients...),
		mailAPIKey: environment[WorkerMailAPIKeyEnv], stubEndpoint: environment[WorkerStubEndpointEnv],
	}
	if _, err := NewHTTPMailer(config.MailerConfig()); err != nil {
		return RuntimeConfig{}, ErrWorkerConfig
	}
	return config, nil
}

func boundedInteger(raw string, defaultValue, minimum, maximum int) (int, error) {
	if raw == "" {
		return defaultValue, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < minimum || value > maximum {
		return 0, ErrWorkerConfig
	}
	return value, nil
}

func validContainer(value string) bool {
	return containerPattern.MatchString(value) && !strings.Contains(value, "--")
}
