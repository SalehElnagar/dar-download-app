package config_test

import (
	"strings"
	"testing"

	"github.com/SalehElnagar/dar-download-app/internal/config"
)

func TestOIDCIssuerValidationIsExactAndHTTPSOnly(t *testing.T) {
	t.Parallel()

	issuerPrefix := "https://identity.example.com/"
	maximumIssuer := issuerPrefix + strings.Repeat("a", config.MaxOIDCIssuerBytes-len(issuerPrefix))
	valid := []string{
		"https://identity.example.com",
		"https://identity.example.com/",
		"https://identity.example.com/realms/customers",
		maximumIssuer,
	}
	for _, issuer := range valid {
		if !config.IsValidOIDCIssuer(issuer) {
			t.Errorf("IsValidOIDCIssuer(%q) = false", issuer)
		}
	}

	invalid := []string{
		"http://identity.example.com",
		"https://user@identity.example.com",
		"https://identity.example.com?tenant=customers",
		"https://identity.example.com?",
		"https://identity.example.com#customers",
		"https://identity.example.com#",
		"https://identity.example.com/%0Aforged",
		"https://",
		"identity.example.com",
		" https://identity.example.com",
		maximumIssuer + "a",
	}
	for _, issuer := range invalid {
		if config.IsValidOIDCIssuer(issuer) {
			t.Errorf("IsValidOIDCIssuer(%q) = true", issuer)
		}
	}

	environment := validEnvironment()
	withoutSlash := strings.TrimSuffix(oidcIssuer, "/")
	environment[config.OIDCIssuerEnv] = withoutSlash
	cfg, err := config.ParseEnvironment(environment)
	if err != nil || cfg.OIDCIssuer != withoutSlash {
		t.Fatalf("trailing slash was normalized: %#v, %v", cfg, err)
	}
}

func TestOIDCSubjectValidationIsOpaqueAndBounded(t *testing.T) {
	t.Parallel()

	valid := []string{
		"subject",
		"Case-Sensitive",
		"customer:42/region",
		"müşteri",
		strings.Repeat("s", config.MaxOIDCSubjectBytes),
	}
	for _, subject := range valid {
		if !config.IsValidOIDCSubject(subject) {
			t.Errorf("IsValidOIDCSubject(%q) = false", subject)
		}
	}

	invalid := []string{
		"",
		"subject\nforged",
		"subject\x7f",
		string([]byte{0xff}),
		strings.Repeat("s", config.MaxOIDCSubjectBytes+1),
	}
	for _, subject := range invalid {
		if config.IsValidOIDCSubject(subject) {
			t.Errorf("IsValidOIDCSubject(%q) = true", subject)
		}
	}
}

func TestLegacyConfigurationInputsHaveNoAlias(t *testing.T) {
	t.Parallel()

	oldPrefix := "HAR" + "MONY_"
	oldOnly := map[string]string{
		oldPrefix + "DAR_TENANT_ID":                  "11111111-1111-4111-8111-111111111111",
		oldPrefix + "DAR_STORAGE_ACCOUNT_NAME":       "stdardownloadpoc01",
		oldPrefix + "DAR_STORAGE_CONTAINER":          "dar-releases",
		oldPrefix + "DAR_MANAGED_IDENTITY_CLIENT_ID": managedIdentityID,
		oldPrefix + "DAR_RELEASES_JSON":              validEnvironment()[config.ReleasesJSONEnv],
	}
	if _, err := config.ParseEnvironment(oldOnly); err == nil {
		t.Fatal("legacy environment names were accepted as aliases")
	}

	environment := validEnvironment()
	legacyField := "allowed_" + "principal_ids"
	environment[config.ReleasesJSONEnv] = `{"dar_01JABCDEF0123456789XYZ":{` +
		`"` + legacyField + `":["` + allowedSubject + `"],` +
		`"blob_name":"releases/example.dar","download_name":"example.dar"}}`
	if _, err := config.ParseEnvironment(environment); err == nil {
		t.Fatal("legacy release authorization field was accepted")
	}
}
