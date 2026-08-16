package config_test

import (
	"os"
	"strings"
	"testing"

	"github.com/SalehElnagar/dar-download-app/internal/config"
)

const customACAProviderName = "DuendePOC"

func customACAEnvironment() map[string]string {
	environment := validEnvironment()
	environment[config.TrustedIdentityModeEnv] = "azure_container_apps_oidc"
	environment["DAR_DOWNLOAD_OIDC_PROVIDER_NAME"] = customACAProviderName
	return environment
}

func TestCustomACAEnvironmentContractIsExact(t *testing.T) {
	t.Parallel()

	if config.OIDCProviderNameEnv != "DAR_DOWNLOAD_OIDC_PROVIDER_NAME" {
		t.Fatalf("OIDCProviderNameEnv = %q", config.OIDCProviderNameEnv)
	}
	if string(config.TrustedIdentityModeAzureContainerAppsOIDC) != "azure_container_apps_oidc" {
		t.Fatalf("custom ACA mode = %q", config.TrustedIdentityModeAzureContainerAppsOIDC)
	}
}

func TestOIDCProviderNameValidationUsesConservativeASCIIBound(t *testing.T) {
	t.Parallel()

	valid := []string{
		"a",
		"DuendePOC01",
		strings.Repeat("A", config.MaxOIDCProviderNameBytes),
	}
	for _, providerName := range valid {
		if !config.IsValidOIDCProviderName(providerName) {
			t.Errorf("IsValidOIDCProviderName(%q) = false", providerName)
		}
	}

	invalid := []string{
		"",
		"aad",
		"AAD",
		"duende-poc",
		"duende_poc",
		"duende.poc",
		"duende/poc",
		" duende",
		"duende ",
		"düende",
		"duende\n",
		strings.Repeat("A", config.MaxOIDCProviderNameBytes+1),
	}
	for _, providerName := range invalid {
		if config.IsValidOIDCProviderName(providerName) {
			t.Errorf("IsValidOIDCProviderName(%q) = true", providerName)
		}
	}
}

func TestParseEnvironmentAcceptsCustomACAOIDCMode(t *testing.T) {
	t.Parallel()

	environment := customACAEnvironment()
	cfg, err := config.ParseEnvironment(environment)
	if err != nil {
		t.Fatalf("ParseEnvironment() error = %v", err)
	}
	if cfg.TrustedIdentityMode != config.TrustedIdentityModeAzureContainerAppsOIDC ||
		cfg.OIDCProviderName != customACAProviderName ||
		cfg.AzureContainerAppsTenantID != "" ||
		cfg.OIDCIssuer != oidcIssuer {
		t.Fatalf("custom ACA configuration = %#v", cfg)
	}
}

func TestLoadEnvironmentReadsCustomACAOIDCMode(t *testing.T) {
	unsetEnvironmentForTest(t, config.AzureContainerAppsTenantIDEnv)
	unsetEnvironmentForTest(t, "DAR_DOWNLOAD_RELEASES_JSON")
	for name, value := range customACAEnvironment() {
		t.Setenv(name, value)
	}
	t.Setenv(config.PortEnv, "8000")

	cfg, err := config.LoadEnvironment()
	if err != nil {
		t.Fatalf("LoadEnvironment() error = %v", err)
	}
	if cfg.TrustedIdentityMode != config.TrustedIdentityModeAzureContainerAppsOIDC ||
		cfg.OIDCProviderName != customACAProviderName {
		t.Fatalf("custom ACA configuration = %#v", cfg)
	}
}

func TestParseEnvironmentRejectsIncompleteOrMixedCustomACAOIDCMode(t *testing.T) {
	t.Parallel()

	tests := map[string]func(map[string]string){
		"missing provider name": func(environment map[string]string) {
			delete(environment, "DAR_DOWNLOAD_OIDC_PROVIDER_NAME")
		},
		"invalid provider name": func(environment map[string]string) {
			environment["DAR_DOWNLOAD_OIDC_PROVIDER_NAME"] = "duende-poc"
		},
		"tenant variable with value": func(environment map[string]string) {
			environment[config.AzureContainerAppsTenantIDEnv] = "11111111-1111-4111-8111-111111111111"
		},
		"tenant variable present but empty": func(environment map[string]string) {
			environment[config.AzureContainerAppsTenantIDEnv] = ""
		},
	}
	for name, mutate := range tests {
		name, mutate := name, mutate
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			environment := customACAEnvironment()
			mutate(environment)
			if _, err := config.ParseEnvironment(environment); err == nil {
				t.Fatal("ParseEnvironment() error = nil, want rejection")
			}
		})
	}
}

func TestParseEnvironmentRejectsStaleProviderNameInExistingModes(t *testing.T) {
	t.Parallel()

	generic := validEnvironment()
	generic["DAR_DOWNLOAD_OIDC_PROVIDER_NAME"] = customACAProviderName
	if _, err := config.ParseEnvironment(generic); err == nil {
		t.Fatal("generic mode accepted custom ACA provider name")
	}

	const tenantID = "11111111-1111-4111-8111-111111111111"
	azure := validEnvironment()
	azure[config.TrustedIdentityModeEnv] = string(config.TrustedIdentityModeAzureContainerApps)
	azure[config.AzureContainerAppsTenantIDEnv] = tenantID
	azure[config.OIDCIssuerEnv] = "https://login.microsoftonline.com/" + tenantID + "/v2.0"
	azure["DAR_DOWNLOAD_OIDC_PROVIDER_NAME"] = customACAProviderName
	if _, err := config.ParseEnvironment(azure); err == nil {
		t.Fatal("Entra ACA mode accepted custom ACA provider name")
	}
}

func unsetEnvironmentForTest(t *testing.T, name string) {
	t.Helper()
	previous, existed := os.LookupEnv(name)
	if err := os.Unsetenv(name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if existed {
			_ = os.Setenv(name, previous)
			return
		}
		_ = os.Unsetenv(name)
	})
}
