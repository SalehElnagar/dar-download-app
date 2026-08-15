package config_test

import (
	"testing"

	"github.com/SalehElnagar/dar-download-app/internal/config"
)

func dynamicEnvironment() map[string]string {
	return map[string]string{
		config.OIDCIssuerEnv:              oidcIssuer,
		config.StorageAccountNameEnv:      "stdardownloadpoc01",
		config.StorageContainerEnv:        "dar-releases",
		config.ManagedIdentityClientIDEnv: managedIdentityID,
	}
}

func TestParseEnvironmentDoesNotRequireReleaseAuthorizationPolicy(t *testing.T) {
	t.Parallel()

	cfg, err := config.ParseEnvironment(dynamicEnvironment())
	if err != nil {
		t.Fatalf("ParseEnvironment() error = %v", err)
	}
	if cfg.OIDCIssuer != oidcIssuer || cfg.StorageContainer != "dar-releases" {
		t.Fatalf("config = %#v", cfg)
	}
}

func TestParseEnvironmentRejectsObsoleteReleaseAuthorizationPolicy(t *testing.T) {
	t.Parallel()

	environment := dynamicEnvironment()
	environment["DAR_DOWNLOAD_RELEASES_JSON"] = `{}`
	if _, err := config.ParseEnvironment(environment); err == nil {
		t.Fatal("obsolete release authorization configuration was accepted")
	}
}
