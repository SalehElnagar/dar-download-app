package config_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/SalehElnagar/dar-download-app/internal/config"
)

const (
	oidcIssuer        = "https://identity.example.com/realms/customers/"
	managedIdentityID = "22222222-2222-4222-8222-222222222222"
	allowedSubject    = "customer:Case-Sensitive-001"
	releaseID         = "dar_01JABCDEF0123456789XYZ"
)

func validEnvironment() map[string]string {
	return map[string]string{
		config.OIDCIssuerEnv:              oidcIssuer,
		config.StorageAccountNameEnv:      "stdardownloadpoc01",
		config.StorageContainerEnv:        "dar-releases",
		config.ManagedIdentityClientIDEnv: managedIdentityID,
		config.ReleasesJSONEnv: `{"dar_01JABCDEF0123456789XYZ":{` +
			`"allowed_subjects":["` + allowedSubject + `"],` +
			`"blob_name":"releases/2026-08/example.dar",` +
			`"download_name":"example.dar"}}`,
	}
}

func TestEnvironmentVariableNamesAreExact(t *testing.T) {
	t.Parallel()

	keys := map[string]string{
		"OIDC issuer":             config.OIDCIssuerEnv,
		"storage account":         config.StorageAccountNameEnv,
		"storage container":       config.StorageContainerEnv,
		"managed identity client": config.ManagedIdentityClientIDEnv,
		"release policy":          config.ReleasesJSONEnv,
		"port":                    config.PortEnv,
	}
	want := map[string]string{
		"OIDC issuer":             "DAR_DOWNLOAD_OIDC_ISSUER",
		"storage account":         "DAR_DOWNLOAD_STORAGE_ACCOUNT_NAME",
		"storage container":       "DAR_DOWNLOAD_STORAGE_CONTAINER",
		"managed identity client": "DAR_DOWNLOAD_MANAGED_IDENTITY_CLIENT_ID",
		"release policy":          "DAR_DOWNLOAD_RELEASES_JSON",
		"port":                    "DAR_DOWNLOAD_PORT",
	}
	for purpose, key := range keys {
		if key != want[purpose] {
			t.Errorf("%s key = %q, want %q", purpose, key, want[purpose])
		}
	}
}

func TestParseEnvironmentAcceptsExactPolicy(t *testing.T) {
	t.Parallel()

	cfg, err := config.ParseEnvironment(validEnvironment())
	if err != nil {
		t.Fatalf("ParseEnvironment() error = %v", err)
	}
	if cfg.OIDCIssuer != oidcIssuer || cfg.ManagedIdentityClientID != managedIdentityID {
		t.Fatalf("identity config = %#v", cfg)
	}
	if cfg.StorageAccountName != "stdardownloadpoc01" || cfg.StorageContainer != "dar-releases" {
		t.Fatalf("storage config = %#v", cfg)
	}
	if cfg.Port != config.DefaultPort {
		t.Fatalf("Port = %d, want %d", cfg.Port, config.DefaultPort)
	}
	release, ok := cfg.Release(releaseID)
	if !ok {
		t.Fatalf("release %q missing", releaseID)
	}
	if release.BlobName != "releases/2026-08/example.dar" || release.DownloadName != "example.dar" {
		t.Fatalf("release = %#v", release)
	}
	if !release.Allows(allowedSubject) || release.Allows(strings.ToLower(allowedSubject)) {
		t.Fatalf("release allowlist = %#v", release)
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

func TestParseEnvironmentRejectsUnsafeOrAmbiguousInputs(t *testing.T) {
	t.Parallel()

	tests := map[string]func(map[string]string){
		"missing required value": func(environment map[string]string) {
			delete(environment, config.OIDCIssuerEnv)
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
		"unknown release field": func(environment map[string]string) {
			environment[config.ReleasesJSONEnv] = `{"dar_01JABCDEF0123456789XYZ":{` +
				`"allowed_subjects":["customer:001"],` +
				`"blob_name":"releases/example.dar","download_name":"example.dar",` +
				`"surprise":true}}`
		},
		"unsafe blob path": func(environment map[string]string) {
			environment[config.ReleasesJSONEnv] = `{"dar_01JABCDEF0123456789XYZ":{` +
				`"allowed_subjects":["customer:001"],` +
				`"blob_name":"../example.dar","download_name":"example.dar"}}`
		},
		"unsafe download name": func(environment map[string]string) {
			environment[config.ReleasesJSONEnv] = `{"dar_01JABCDEF0123456789XYZ":{` +
				`"allowed_subjects":["customer:001"],` +
				`"blob_name":"releases/example.dar","download_name":"example.txt"}}`
		},
		"empty allowlist": func(environment map[string]string) {
			environment[config.ReleasesJSONEnv] = `{"dar_01JABCDEF0123456789XYZ":{` +
				`"allowed_subjects":[],"blob_name":"releases/example.dar",` +
				`"download_name":"example.dar"}}`
		},
		"duplicate subject": func(environment map[string]string) {
			environment[config.ReleasesJSONEnv] = `{"dar_01JABCDEF0123456789XYZ":{` +
				`"allowed_subjects":["customer:001","customer:001"],` +
				`"blob_name":"releases/example.dar","download_name":"example.dar"}}`
		},
		"control subject": func(environment map[string]string) {
			environment[config.ReleasesJSONEnv] = `{"dar_01JABCDEF0123456789XYZ":{` +
				`"allowed_subjects":["customer:\u0001"],` +
				`"blob_name":"releases/example.dar","download_name":"example.dar"}}`
		},
		"empty subject": func(environment map[string]string) {
			environment[config.ReleasesJSONEnv] = `{"dar_01JABCDEF0123456789XYZ":{` +
				`"allowed_subjects":[""],` +
				`"blob_name":"releases/example.dar","download_name":"example.dar"}}`
		},
		"oversized subject": func(environment map[string]string) {
			environment[config.ReleasesJSONEnv] = `{"dar_01JABCDEF0123456789XYZ":{` +
				`"allowed_subjects":["` + strings.Repeat("s", config.MaxOIDCSubjectBytes+1) + `"],` +
				`"blob_name":"releases/example.dar","download_name":"example.dar"}}`
		},
		"oversized policy": func(environment map[string]string) {
			environment[config.ReleasesJSONEnv] = `{"x":"` + strings.Repeat("a", 65*1024) + `"}`
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

func TestParseEnvironmentRejectsDuplicateBlobAndDownloadNames(t *testing.T) {
	t.Parallel()

	environment := validEnvironment()
	environment[config.ReleasesJSONEnv] = `{
      "dar_01JABCDEF0123456789XYZ": {
        "allowed_subjects": ["customer:001"],
        "blob_name": "releases/example.dar",
        "download_name": "example.dar"
      },
      "dar_01JABCDEF0123456789XYA": {
        "allowed_subjects": ["customer:002"],
        "blob_name": "releases/example.dar",
        "download_name": "example.dar"
      }
    }`

	if _, err := config.ParseEnvironment(environment); err == nil {
		t.Fatal("ParseEnvironment() error = nil, want duplicate rejection")
	}
}

func TestParseEnvironmentPreservesDistinctSubjectCase(t *testing.T) {
	t.Parallel()

	environment := validEnvironment()
	environment[config.ReleasesJSONEnv] = `{"dar_01JABCDEF0123456789XYZ":{` +
		`"allowed_subjects":["Customer:001","customer:001"],` +
		`"blob_name":"releases/example.dar","download_name":"example.dar"}}`
	cfg, err := config.ParseEnvironment(environment)
	if err != nil {
		t.Fatalf("ParseEnvironment() error = %v", err)
	}
	release, ok := cfg.Release(releaseID)
	if !ok || !release.Allows("Customer:001") || !release.Allows("customer:001") {
		t.Fatalf("release case-sensitive allowlist = %#v", release)
	}
}

func TestLoadEnvironmentReadsOnlyDocumentedValues(t *testing.T) {
	environment := validEnvironment()
	for name, value := range environment {
		t.Setenv(name, value)
	}
	t.Setenv(config.PortEnv, "8081")
	cfg, err := config.LoadEnvironment()
	if err != nil {
		t.Fatalf("LoadEnvironment() error = %v", err)
	}
	if cfg.Port != 8081 || cfg.ReleaseCount() != 1 || cfg.OIDCIssuer != oidcIssuer {
		t.Fatalf("config = %#v", cfg)
	}
}

func TestParseEnvironmentRejectsAdditionalUnsafeBlobForms(t *testing.T) {
	t.Parallel()

	unsafeNames := []string{
		"/absolute/example.dar",
		"releases/example.dar/",
		"releases//example.dar",
		"releases/./example.dar",
		"releases/../example.dar",
		`releases\\example.dar`,
		"releases/example.dar?query",
		"releases/example.dar#fragment",
		"releases/\x1fexample.dar",
		strings.Repeat("a", config.MaxBlobNameBytes+1),
	}
	for _, blobName := range unsafeNames {
		blobName := blobName
		t.Run("unsafe blob", func(t *testing.T) {
			t.Parallel()
			environment := validEnvironment()
			environment[config.ReleasesJSONEnv] = `{"dar_01JABCDEF0123456789XYZ":{` +
				`"allowed_subjects":["customer:001"],` +
				`"blob_name":` + strconv.Quote(blobName) + `,"download_name":"example.dar"}}`
			if _, err := config.ParseEnvironment(environment); err == nil {
				t.Fatalf("accepted unsafe blob name %q", blobName)
			}
		})
	}
}

func TestParseEnvironmentRejectsDuplicateJSONKeys(t *testing.T) {
	t.Parallel()

	environment := validEnvironment()
	environment[config.ReleasesJSONEnv] = `{"dar_01JABCDEF0123456789XYZ":{` +
		`"allowed_subjects":["customer:001"],` +
		`"blob_name":"releases/example.dar","blob_name":"releases/other.dar",` +
		`"download_name":"example.dar"}}`
	if _, err := config.ParseEnvironment(environment); err == nil {
		t.Fatal("accepted duplicate JSON key")
	}
}
