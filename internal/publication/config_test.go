package publication

import "testing"

func TestParseEnvironmentBuildsSecretlessPublisherConfiguration(t *testing.T) {
	t.Parallel()
	config, err := ParseEnvironment(publisherEnvironment())
	if err != nil {
		t.Fatalf("ParseEnvironment() error = %v", err)
	}
	if config.Repository != "SalehElnagar/dar-download-app" ||
		config.CreatedAt != "2026-08-17T19:00:00Z" ||
		config.RecipientsFile != "/agent/temp/notification-recipients.csv" ||
		config.ServiceBusNamespace != "sb-dar-prod.servicebus.windows.net" {
		t.Fatalf("config = %#v", config)
	}
}

func TestParseEnvironmentRejectsMissingOrSecretLikeConfiguration(t *testing.T) {
	t.Parallel()
	for _, name := range []string{
		EnvRepository, EnvCommitSHA, EnvRecipientsFile, EnvStorageAccount,
		EnvServiceBusNamespace, EnvApplicationOrigin,
	} {
		environment := publisherEnvironment()
		delete(environment, name)
		if _, err := ParseEnvironment(environment); err == nil {
			t.Fatalf("ParseEnvironment() accepted missing %s", name)
		}
	}
	environment := publisherEnvironment()
	environment["DAR_PUBLISHER_STORAGE_KEY"] = "forbidden"
	if _, err := ParseEnvironment(environment); err == nil {
		t.Fatal("ParseEnvironment() accepted a storage credential setting")
	}
}

func publisherEnvironment() map[string]string {
	return map[string]string{
		EnvRepository:          "SalehElnagar/dar-download-app",
		EnvCommitSHA:           "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		EnvCommitEpoch:         "1786993200",
		EnvRepositoryRoot:      "/agent/source",
		EnvReleaseID:           "dar_distribution_01",
		EnvRecipientsFile:      "/agent/temp/notification-recipients.csv",
		EnvStorageAccount:      "stdarproduction01",
		EnvReleasesContainer:   "dar-releases",
		EnvManifestsContainer:  "dar-release-manifests",
		EnvBatchesContainer:    "dar-recipient-batches",
		EnvServiceBusNamespace: "sb-dar-prod.servicebus.windows.net",
		EnvServiceBusQueue:     "dar-release-notifications",
		EnvApplicationOrigin:   "https://download.example.internal",
	}
}
