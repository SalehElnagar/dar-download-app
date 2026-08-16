package repository_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublishedContractsDescribeCustomACAOIDCBoundary(t *testing.T) {
	root := repositoryRoot(t)
	requiredByFile := map[string][]string{
		"README.md": {
			"azure_container_apps_oidc",
			"DAR_DOWNLOAD_OIDC_PROVIDER_NAME",
			"/.auth/login/<provider-name>/callback",
			"Container Apps secret",
			"az containerapp auth openid-connect add",
			"--client-secret-name",
			"GrantTypes.Code",
			"RequirePkce",
			"AllowOfflineAccess",
			"AllowAccessTokensViaBrowser",
			"current Entra POC remains",
		},
		"api/openapi.yaml": {
			"azure_container_apps_oidc",
			"custom OpenID Connect",
		},
		"docs/configuration.md": {
			"DAR_DOWNLOAD_OIDC_PROVIDER_NAME",
			"32-byte",
			"X-MS-CLIENT-PRINCIPAL-IDP",
			"client secret",
		},
		"docs/operations.md": {
			"/.auth/login/<provider-name>/callback",
			"authorization code",
			"clientSecretSettingName",
			"Duende",
		},
		"docs/threat-model.md": {
			"custom OpenID Connect",
			"client secret",
		},
		"docs/assurance-map.md": {
			"azure_container_apps_oidc",
		},
	}
	for relative, requiredFragments := range requiredByFile {
		content, err := os.ReadFile(filepath.Join(root, relative))
		if err != nil {
			t.Fatal(err)
		}
		for _, required := range requiredFragments {
			if !strings.Contains(strings.ToLower(string(content)), strings.ToLower(required)) {
				t.Errorf("%s is missing %q", relative, required)
			}
		}
	}
}

func TestGoRuntimeHasNoCustomOIDCClientCredentialInputs(t *testing.T) {
	root := repositoryRoot(t)
	targets := []string{
		"cmd/dar-download/main.go",
		"internal/auth/oidc.go",
		"internal/auth/azure_container_apps_oidc.go",
		"internal/config/config.go",
		"Dockerfile",
	}
	for _, relative := range targets {
		content, err := os.ReadFile(filepath.Join(root, relative))
		if err != nil {
			if os.IsNotExist(err) && relative == "internal/auth/azure_container_apps_oidc.go" {
				continue
			}
			t.Fatal(err)
		}
		for _, forbidden := range []string{
			"DAR_DOWNLOAD_OIDC_CLIENT_ID",
			"DAR_DOWNLOAD_OIDC_CLIENT_SECRET",
			"DAR_DOWNLOAD_OIDC_AUTHORIZATION_ENDPOINT",
			"DAR_DOWNLOAD_OIDC_TOKEN_ENDPOINT",
			"clientSecretSettingName",
		} {
			if strings.Contains(string(content), forbidden) {
				t.Errorf("%s contains prohibited Go credential or flow input %q", relative, forbidden)
			}
		}
	}
}

func TestSecurityWorkflowRunsCustomACAOIDCFuzzer(t *testing.T) {
	root := repositoryRoot(t)
	exactSelectors := map[string][]string{
		"Makefile": {
			"-fuzz='^FuzzAuthenticateAzureContainerApps$$'",
			"-fuzz='^FuzzAuthenticateAzureContainerAppsOIDC$$'",
		},
		"scripts/prebuild.sh": {
			"-fuzz='^FuzzAuthenticateAzureContainerApps$'",
			"-fuzz='^FuzzAuthenticateAzureContainerAppsOIDC$'",
		},
	}
	for relative, selectors := range exactSelectors {
		content, err := os.ReadFile(filepath.Join(root, relative))
		if err != nil {
			t.Fatal(err)
		}
		for _, exactSelector := range selectors {
			if !strings.Contains(string(content), exactSelector) {
				t.Errorf("%s does not enforce exact selector %q", relative, exactSelector)
			}
		}
	}
	workflowControl, err := os.ReadFile(filepath.Join(root, "scripts/test-security-workflow.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(workflowControl), "FuzzAuthenticateAzureContainerAppsOIDC") {
		t.Error("scripts/test-security-workflow.sh does not enforce the custom ACA OIDC fuzzer")
	}
}
