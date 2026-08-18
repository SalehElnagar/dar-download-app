package publication

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	EnvRepository          = "DAR_PUBLISHER_REPOSITORY"
	EnvCommitSHA           = "DAR_PUBLISHER_SOURCE_COMMIT_SHA"
	EnvCommitEpoch         = "DAR_PUBLISHER_SOURCE_COMMIT_EPOCH"
	EnvRepositoryRoot      = "DAR_PUBLISHER_REPOSITORY_ROOT"
	EnvReleaseID           = "DAR_PUBLISHER_RELEASE_ID"
	EnvRecipientsFile      = "DAR_PUBLISHER_RECIPIENTS_FILE"
	EnvStorageAccount      = "DAR_PUBLISHER_STORAGE_ACCOUNT_NAME"
	EnvReleasesContainer   = "DAR_PUBLISHER_RELEASES_CONTAINER"
	EnvManifestsContainer  = "DAR_PUBLISHER_MANIFESTS_CONTAINER"
	EnvBatchesContainer    = "DAR_PUBLISHER_BATCHES_CONTAINER"
	EnvServiceBusNamespace = "DAR_PUBLISHER_SERVICEBUS_NAMESPACE"
	EnvServiceBusQueue     = "DAR_PUBLISHER_SERVICEBUS_QUEUE"
	EnvApplicationOrigin   = "DAR_PUBLISHER_APPLICATION_ORIGIN"
)

var (
	ErrEnvironment             = errors.New("invalid publisher environment")
	storageAccountPattern      = regexp.MustCompile(`^[a-z0-9]{3,24}$`)
	serviceBusNamespacePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{4,48}[a-z0-9]\.servicebus\.windows\.net$`)
	queueNamePattern           = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,258}[A-Za-z0-9]$`)
)

// Environment is the complete secretless release-publisher runtime configuration.
type Environment struct {
	ApplicationOrigin   string
	BatchesContainer    string
	CommitSHA           string
	CreatedAt           string
	ManifestsContainer  string
	RecipientsFile      string
	ReleaseID           string
	ReleasesContainer   string
	Repository          string
	RepositoryRoot      string
	ServiceBusNamespace string
	ServiceBusQueue     string
	StorageAccount      string
}

// PublisherConfig returns the bounded static publication policy.
func (environment Environment) PublisherConfig() Config {
	return Config{
		Repository: environment.Repository, ReleasesContainer: environment.ReleasesContainer,
		ManifestsContainer: environment.ManifestsContainer, BatchesContainer: environment.BatchesContainer,
		ApplicationOrigin: environment.ApplicationOrigin, BatchSize: maxPublicationBatchSize,
	}
}

// ParseEnvironment rejects credentials and derives the publication time from the source commit.
func ParseEnvironment(values map[string]string) (Environment, error) {
	if values == nil {
		return Environment{}, ErrEnvironment
	}
	for _, forbidden := range []string{
		"DAR_PUBLISHER_STORAGE_KEY", "DAR_PUBLISHER_STORAGE_CONNECTION_STRING",
		"DAR_PUBLISHER_SERVICEBUS_CONNECTION_STRING", "DAR_PUBLISHER_SENDGRID_API_KEY",
	} {
		if strings.TrimSpace(values[forbidden]) != "" {
			return Environment{}, ErrEnvironment
		}
	}
	required := []string{
		EnvRepository, EnvCommitSHA, EnvCommitEpoch, EnvRepositoryRoot, EnvReleaseID,
		EnvRecipientsFile, EnvStorageAccount, EnvReleasesContainer, EnvManifestsContainer,
		EnvBatchesContainer, EnvServiceBusNamespace, EnvServiceBusQueue, EnvApplicationOrigin,
	}
	for _, name := range required {
		if strings.TrimSpace(values[name]) == "" || containsControl(values[name]) {
			return Environment{}, ErrEnvironment
		}
	}
	epoch, err := strconv.ParseInt(values[EnvCommitEpoch], 10, 64)
	if err != nil || epoch < 946684800 || epoch > 4102444799 {
		return Environment{}, ErrEnvironment
	}
	namespace := strings.ToLower(values[EnvServiceBusNamespace])
	containers := []string{
		values[EnvReleasesContainer], values[EnvManifestsContainer], values[EnvBatchesContainer],
	}
	seen := make(map[string]struct{}, len(containers))
	for _, container := range containers {
		if !containerPattern.MatchString(container) || strings.Contains(container, "--") {
			return Environment{}, ErrEnvironment
		}
		if _, duplicate := seen[container]; duplicate {
			return Environment{}, ErrEnvironment
		}
		seen[container] = struct{}{}
	}
	if !repositoryPattern.MatchString(values[EnvRepository]) ||
		!commitPattern.MatchString(values[EnvCommitSHA]) ||
		!releaseIDPattern.MatchString(values[EnvReleaseID]) ||
		!storageAccountPattern.MatchString(values[EnvStorageAccount]) ||
		!serviceBusNamespacePattern.MatchString(namespace) ||
		!queueNamePattern.MatchString(values[EnvServiceBusQueue]) ||
		strings.Contains(values[EnvServiceBusQueue], "//") ||
		strings.Contains(values[EnvServiceBusQueue], "..") ||
		!validApplicationOrigin(values[EnvApplicationOrigin]) {
		return Environment{}, ErrEnvironment
	}
	return Environment{
		ApplicationOrigin: strings.TrimSuffix(values[EnvApplicationOrigin], "/"),
		BatchesContainer:  values[EnvBatchesContainer], CommitSHA: values[EnvCommitSHA],
		CreatedAt:          time.Unix(epoch, 0).UTC().Format(time.RFC3339),
		ManifestsContainer: values[EnvManifestsContainer], RecipientsFile: values[EnvRecipientsFile],
		ReleaseID: values[EnvReleaseID], ReleasesContainer: values[EnvReleasesContainer],
		Repository: values[EnvRepository], RepositoryRoot: values[EnvRepositoryRoot],
		ServiceBusNamespace: namespace, ServiceBusQueue: values[EnvServiceBusQueue],
		StorageAccount: values[EnvStorageAccount],
	}, nil
}

func containsControl(value string) bool {
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return true
		}
	}
	return false
}
