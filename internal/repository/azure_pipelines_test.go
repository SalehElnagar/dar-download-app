package repository_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleasePipelineUsesSecureRecipientInputAndProtectedAzurePublication(t *testing.T) {
	t.Parallel()
	path := filepath.Join(repositoryRoot(t), "azure-pipelines", "dar-release-distribution.yml")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, required := range []string{
		"releases/**",
		"refs/heads/main",
		"Build.Repository.Provider",
		"lfs: true",
		"DownloadSecureFile@1",
		"secureFilePath",
		"GoTool@0",
		"go run ./cmd/dar-release-publisher validate",
		"validated-release-input/dar-release-publisher",
		"publisher.sha256",
		"AzureCLI@2",
		"DAR_AZURE_WORKLOAD_IDENTITY_SERVICE_CONNECTION",
		"environment: dar-release-distribution-production",
		"name: $(DAR_PRIVATE_AGENT_POOL)",
		"harmony.private-network -equals true",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("pipeline missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"sendgrid", "api_key", "connection_string", "notification-recipients.csv",
		"vmImage: ubuntu-24.04\n    jobs:\n      - deployment:",
	} {
		if strings.Contains(strings.ToLower(text), strings.ToLower(forbidden)) {
			t.Errorf("pipeline contains forbidden fragment %q", forbidden)
		}
	}
}

func TestCIPipelineRunsRepositoryOwnedSecurityCandidate(t *testing.T) {
	t.Parallel()
	path := filepath.Join(repositoryRoot(t), "azure-pipelines", "ci.yml")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, required := range []string{
		"trigger:", "pr:", "checkout: self", "persistCredentials: false",
		"lfs: true",
		"./scripts/bootstrap-tools.sh", "./scripts/prebuild.sh", "./scripts/build-image.sh",
		"./scripts/postbuild.sh", "./scripts/build-worker-image.sh",
		"./scripts/postbuild-worker.sh", "dar-release-publisher validate",
		"notification-recipients.example.csv", "PublishPipelineArtifact@1",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("pipeline missing %q", required)
		}
	}
}

func TestRepositoryContainsNoRealRecipientFile(t *testing.T) {
	t.Parallel()
	directory := filepath.Join(repositoryRoot(t), "recipients")
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".csv") &&
			!strings.HasSuffix(entry.Name(), ".example.csv") {
			t.Errorf("recipient data file must not be committed: %s", entry.Name())
		}
	}
}
