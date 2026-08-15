package config_test

import (
	"os"
	"testing"

	"github.com/SalehElnagar/dar-download-app/internal/config"
)

const (
	oidcIssuer        = "https://identity.example.com/realms/customers/"
	managedIdentityID = "22222222-2222-4222-8222-222222222222"
)

func validEnvironment() map[string]string {
	return map[string]string{
		config.OIDCIssuerEnv:              oidcIssuer,
		config.StorageAccountNameEnv:      "stdardownloadpoc01",
		config.StorageContainerEnv:        "dar-releases",
		config.ManagedIdentityClientIDEnv: managedIdentityID,
	}
}

func TestEnvironmentVariableNamesAreExact(t *testing.T) {
	t.Parallel()

	keys := map[string]string{
		"trusted identity mode":       config.TrustedIdentityModeEnv,
		"OIDC issuer":                 config.OIDCIssuerEnv,
		"Azure Container Apps tenant": config.AzureContainerAppsTenantIDEnv,
		"storage account":             config.StorageAccountNameEnv,
		"storage container":           config.StorageContainerEnv,
		"managed identity client":     config.ManagedIdentityClientIDEnv,
		"port":                        config.PortEnv,
	}
	want := map[string]string{
		"trusted identity mode":       "DAR_DOWNLOAD_TRUSTED_IDENTITY_MODE",
		"OIDC issuer":                 "DAR_DOWNLOAD_OIDC_ISSUER",
		"Azure Container Apps tenant": "DAR_DOWNLOAD_AZURE_CONTAINER_APPS_TENANT_ID",
		"storage account":             "DAR_DOWNLOAD_STORAGE_ACCOUNT_NAME",
		"storage container":           "DAR_DOWNLOAD_STORAGE_CONTAINER",
		"managed identity client":     "DAR_DOWNLOAD_MANAGED_IDENTITY_CLIENT_ID",
		"port":                        "DAR_DOWNLOAD_PORT",
	}
	for purpose, key := range keys {
		if key != want[purpose] {
			t.Errorf("%s key = %q, want %q", purpose, key, want[purpose])
		}
	}
}

func TestParseEnvironmentAcceptsCompleteConfiguration(t *testing.T) {
	t.Parallel()

	cfg, err := config.ParseEnvironment(validEnvironment())
	if err != nil {
		t.Fatalf("ParseEnvironment() error = %v", err)
	}
	if cfg.OIDCIssuer != oidcIssuer || cfg.ManagedIdentityClientID != managedIdentityID {
		t.Fatalf("identity config = %#v", cfg)
	}
	if cfg.TrustedIdentityMode != config.TrustedIdentityModeOIDCHeaders ||
		cfg.AzureContainerAppsTenantID != "" {
		t.Fatalf("trusted identity config = %#v", cfg)
	}
	if cfg.StorageAccountName != "stdardownloadpoc01" || cfg.StorageContainer != "dar-releases" {
		t.Fatalf("storage config = %#v", cfg)
	}
	if cfg.Port != config.DefaultPort {
		t.Fatalf("Port = %d, want %d", cfg.Port, config.DefaultPort)
	}
}

func TestParseEnvironmentAcceptsExplicitTrustedIdentityModes(t *testing.T) {
	t.Parallel()

	generic := validEnvironment()
	generic[config.TrustedIdentityModeEnv] = string(config.TrustedIdentityModeOIDCHeaders)
	genericConfig, err := config.ParseEnvironment(generic)
	if err != nil || genericConfig.TrustedIdentityMode != config.TrustedIdentityModeOIDCHeaders {
		t.Fatalf("generic ParseEnvironment() = %#v, %v", genericConfig, err)
	}

	const tenantID = "11111111-1111-4111-8111-111111111111"
	azure := validEnvironment()
	azure[config.TrustedIdentityModeEnv] = string(config.TrustedIdentityModeAzureContainerApps)
	azure[config.AzureContainerAppsTenantIDEnv] = tenantID
	azure[config.OIDCIssuerEnv] = "https://login.microsoftonline.com/" + tenantID + "/v2.0"
	azureConfig, err := config.ParseEnvironment(azure)
	if err != nil {
		t.Fatalf("Azure ParseEnvironment() error = %v", err)
	}
	if azureConfig.TrustedIdentityMode != config.TrustedIdentityModeAzureContainerApps ||
		azureConfig.AzureContainerAppsTenantID != tenantID ||
		azureConfig.OIDCIssuer != azure[config.OIDCIssuerEnv] {
		t.Fatalf("Azure trusted identity config = %#v", azureConfig)
	}
}

func TestParseEnvironmentRejectsAmbiguousTrustedIdentityModes(t *testing.T) {
	t.Parallel()

	const tenantID = "11111111-1111-4111-8111-111111111111"
	tests := map[string]func(map[string]string){
		"unknown mode": func(environment map[string]string) {
			environment[config.TrustedIdentityModeEnv] = "automatic"
		},
		"generic mode with Azure tenant": func(environment map[string]string) {
			environment[config.TrustedIdentityModeEnv] = string(config.TrustedIdentityModeOIDCHeaders)
			environment[config.AzureContainerAppsTenantIDEnv] = tenantID
		},
		"Azure mode without tenant": func(environment map[string]string) {
			environment[config.TrustedIdentityModeEnv] = string(config.TrustedIdentityModeAzureContainerApps)
		},
		"Azure mode with invalid tenant": func(environment map[string]string) {
			environment[config.TrustedIdentityModeEnv] = string(config.TrustedIdentityModeAzureContainerApps)
			environment[config.AzureContainerAppsTenantIDEnv] = "not-a-tenant"
		},
		"Azure mode with unrelated issuer": func(environment map[string]string) {
			environment[config.TrustedIdentityModeEnv] = string(config.TrustedIdentityModeAzureContainerApps)
			environment[config.AzureContainerAppsTenantIDEnv] = tenantID
		},
		"Azure mode with wrong issuer tenant": func(environment map[string]string) {
			environment[config.TrustedIdentityModeEnv] = string(config.TrustedIdentityModeAzureContainerApps)
			environment[config.AzureContainerAppsTenantIDEnv] = tenantID
			environment[config.OIDCIssuerEnv] = "https://login.microsoftonline.com/22222222-2222-4222-8222-222222222222/v2.0"
		},
		"Azure mode with trailing issuer slash": func(environment map[string]string) {
			environment[config.TrustedIdentityModeEnv] = string(config.TrustedIdentityModeAzureContainerApps)
			environment[config.AzureContainerAppsTenantIDEnv] = tenantID
			environment[config.OIDCIssuerEnv] = "https://login.microsoftonline.com/" + tenantID + "/v2.0/"
		},
	}
	for name, mutate := range tests {
		name, mutate := name, mutate
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			environment := validEnvironment()
			mutate(environment)
			if _, err := config.ParseEnvironment(environment); err == nil {
				t.Fatal("ParseEnvironment() error = nil, want rejection")
			}
		})
	}
}

func TestParseEnvironmentAcceptsExplicitPort(t *testing.T) {
	t.Parallel()

	environment := validEnvironment()
	environment[config.PortEnv] = "9443"
	cfg, err := config.ParseEnvironment(environment)
	if err != nil {
		t.Fatalf("ParseEnvironment() error = %v", err)
	}
	if cfg.Port != 9443 {
		t.Fatalf("Port = %d, want 9443", cfg.Port)
	}
}

func TestParseEnvironmentRejectsUnsafeOrMissingInputs(t *testing.T) {
	t.Parallel()

	tests := map[string]func(map[string]string){
		"missing issuer": func(environment map[string]string) {
			delete(environment, config.OIDCIssuerEnv)
		},
		"missing storage account": func(environment map[string]string) {
			delete(environment, config.StorageAccountNameEnv)
		},
		"missing storage container": func(environment map[string]string) {
			delete(environment, config.StorageContainerEnv)
		},
		"missing managed identity": func(environment map[string]string) {
			delete(environment, config.ManagedIdentityClientIDEnv)
		},
		"invalid issuer": func(environment map[string]string) {
			environment[config.OIDCIssuerEnv] = "http://identity.example.com"
		},
		"invalid managed identity": func(environment map[string]string) {
			environment[config.ManagedIdentityClientIDEnv] = "not-a-client-id"
		},
		"bad account": func(environment map[string]string) {
			environment[config.StorageAccountNameEnv] = "Bad_Account"
		},
		"bad container": func(environment map[string]string) {
			environment[config.StorageContainerEnv] = "-bad-container"
		},
		"invalid port": func(environment map[string]string) {
			environment[config.PortEnv] = "0"
		},
	}
	for name, mutate := range tests {
		name, mutate := name, mutate
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			environment := validEnvironment()
			mutate(environment)
			if _, err := config.ParseEnvironment(environment); err == nil {
				t.Fatal("ParseEnvironment() error = nil, want rejection")
			}
		})
	}
}

func TestLoadEnvironmentReadsDocumentedValues(t *testing.T) {
	previous, existed := os.LookupEnv("DAR_DOWNLOAD_RELEASES_JSON")
	if err := os.Unsetenv("DAR_DOWNLOAD_RELEASES_JSON"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if existed {
			_ = os.Setenv("DAR_DOWNLOAD_RELEASES_JSON", previous)
		} else {
			_ = os.Unsetenv("DAR_DOWNLOAD_RELEASES_JSON")
		}
	})
	for name, value := range validEnvironment() {
		t.Setenv(name, value)
	}
	t.Setenv(config.PortEnv, "8081")
	cfg, err := config.LoadEnvironment()
	if err != nil {
		t.Fatalf("LoadEnvironment() error = %v", err)
	}
	if cfg.Port != 8081 || cfg.OIDCIssuer != oidcIssuer {
		t.Fatalf("config = %#v", cfg)
	}
}

func TestLoadEnvironmentRejectsObsoleteReleasePolicy(t *testing.T) {
	for name, value := range validEnvironment() {
		t.Setenv(name, value)
	}
	t.Setenv("DAR_DOWNLOAD_RELEASES_JSON", `{}`)
	if _, err := config.LoadEnvironment(); err == nil {
		t.Fatal("LoadEnvironment() accepted obsolete release policy")
	}
}
