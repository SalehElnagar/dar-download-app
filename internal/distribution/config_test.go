package distribution

import (
	"encoding/base64"
	"fmt"
	"testing"
)

func TestParseWorkerEnvironmentBuildsSecretSafeStubPolicy(t *testing.T) {
	t.Parallel()
	environment := workerEnvironment()

	config, err := ParseWorkerEnvironment(environment)
	if err != nil {
		t.Fatalf("ParseWorkerEnvironment() error = %v", err)
	}
	if config.Provider() != "stub" || config.ServiceBusQueue != "dar-release-notifications" ||
		len(config.HMACKey()) != 32 {
		t.Fatalf("config = %s", config)
	}
	rendered := fmt.Sprintf("%+v", config)
	if stringContains(rendered, environment[WorkerHMACKeyEnv]) || stringContains(rendered, "kkkkkkkk") {
		t.Fatalf("config formatting exposed secret: %s", rendered)
	}
}

func TestParseWorkerEnvironmentRequiresProviderKeyOutsideStubMode(t *testing.T) {
	t.Parallel()
	environment := workerEnvironment()
	environment[WorkerMailModeEnv] = string(MailModeSandbox)
	delete(environment, WorkerStubEndpointEnv)

	if _, err := ParseWorkerEnvironment(environment); err == nil {
		t.Fatal("ParseWorkerEnvironment() accepted missing provider key")
	}
	environment[WorkerMailAPIKeyEnv] = "unit-provider-value"
	if _, err := ParseWorkerEnvironment(environment); err != nil {
		t.Fatalf("ParseWorkerEnvironment() error = %v", err)
	}
}

func TestParseWorkerEnvironmentRejectsNoncanonicalHMACEncoding(t *testing.T) {
	t.Parallel()
	environment := workerEnvironment()
	environment[WorkerHMACKeyEnv] = base64.RawStdEncoding.EncodeToString([]byte("short"))

	if _, err := ParseWorkerEnvironment(environment); err == nil {
		t.Fatal("ParseWorkerEnvironment() accepted invalid HMAC key")
	}
}

func workerEnvironment() map[string]string {
	return map[string]string{
		WorkerIdentityClientIDEnv:    "11111111-1111-4111-8111-111111111111",
		WorkerStorageAccountEnv:      "stdardistribution01",
		WorkerServiceBusNamespaceEnv: "sb-dar-poc.servicebus.windows.net",
		WorkerServiceBusQueueEnv:     "dar-release-notifications",
		WorkerManifestsContainerEnv:  "dar-release-manifests",
		WorkerBatchesContainerEnv:    "dar-recipient-batches",
		WorkerReceiptsContainerEnv:   "dar-notification-receipts",
		WorkerHMACKeyEnv:             base64.StdEncoding.EncodeToString([]byte("kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkk")),
		WorkerHMACKeyVersionEnv:      "v1",
		WorkerMailModeEnv:            string(MailModeStub),
		WorkerMailFromEmailEnv:       "dar-poc@example.com",
		WorkerMailFromNameEnv:        "DAR POC",
		WorkerAllowedRecipientsEnv:   `["ava.example@example.com","noah.sample@example.com"]`,
		WorkerStubEndpointEnv:        "https://mail-stub.example.internal/v3/mail/send",
	}
}
