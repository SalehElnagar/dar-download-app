package config_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/SalehElnagar/dar-download-app/internal/config"
)

const (
	tenantID          = "11111111-1111-4111-8111-111111111111"
	managedIdentityID = "22222222-2222-4222-8222-222222222222"
	principalID       = "33333333-3333-4333-8333-333333333333"
	releaseID         = "dar_01JABCDEF0123456789XYZ"
)

func validEnvironment() map[string]string {
	return map[string]string{
		config.TenantIDEnv:                tenantID,
		config.StorageAccountNameEnv:      "stdardownloadpoc01",
		config.StorageContainerEnv:        "dar-releases",
		config.ManagedIdentityClientIDEnv: managedIdentityID,
		config.ReleasesJSONEnv: `{"dar_01JABCDEF0123456789XYZ":{` +
			`"allowed_principal_ids":["33333333-3333-4333-8333-333333333333"],` +
			`"blob_name":"releases/2026-08/example.dar",` +
			`"download_name":"example.dar"}}`,
	}
}

func TestParseEnvironmentAcceptsExactPolicy(t *testing.T) {
	t.Parallel()

	cfg, err := config.ParseEnvironment(validEnvironment())
	if err != nil {
		t.Fatalf("ParseEnvironment() error = %v", err)
	}
	if cfg.TenantID != tenantID || cfg.ManagedIdentityClientID != managedIdentityID {
		t.Fatalf("identity config = %#v", cfg)
	}
	if cfg.StorageAccountName != "stdardownloadpoc01" || cfg.StorageContainer != "dar-releases" {
		t.Fatalf("storage config = %#v", cfg)
	}
	if cfg.Port != 8000 {
		t.Fatalf("Port = %d, want 8000", cfg.Port)
	}
	release, ok := cfg.Release(releaseID)
	if !ok {
		t.Fatalf("release %q missing", releaseID)
	}
	if release.BlobName != "releases/2026-08/example.dar" || release.DownloadName != "example.dar" {
		t.Fatalf("release = %#v", release)
	}
	if !release.Allows(principalID) || release.Allows(managedIdentityID) {
		t.Fatalf("release allowlist = %#v", release)
	}
}

func TestParseEnvironmentAcceptsExplicitPort(t *testing.T) {
	t.Parallel()

	env := validEnvironment()
	env[config.PortEnv] = "9443"
	cfg, err := config.ParseEnvironment(env)
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
		"missing required value": func(env map[string]string) {
			delete(env, config.TenantIDEnv)
		},
		"non canonical tenant": func(env map[string]string) {
			env[config.TenantIDEnv] = "AAAAAAAA-1111-4111-8111-111111111111"
		},
		"bad account": func(env map[string]string) {
			env[config.StorageAccountNameEnv] = "Bad_Account"
		},
		"bad container": func(env map[string]string) {
			env[config.StorageContainerEnv] = "-bad-container"
		},
		"invalid port": func(env map[string]string) {
			env[config.PortEnv] = "0"
		},
		"unknown release field": func(env map[string]string) {
			env[config.ReleasesJSONEnv] = `{"dar_01JABCDEF0123456789XYZ":{` +
				`"allowed_principal_ids":["33333333-3333-4333-8333-333333333333"],` +
				`"blob_name":"releases/example.dar","download_name":"example.dar",` +
				`"surprise":true}}`
		},
		"unsafe blob path": func(env map[string]string) {
			env[config.ReleasesJSONEnv] = `{"dar_01JABCDEF0123456789XYZ":{` +
				`"allowed_principal_ids":["33333333-3333-4333-8333-333333333333"],` +
				`"blob_name":"../example.dar","download_name":"example.dar"}}`
		},
		"unsafe download name": func(env map[string]string) {
			env[config.ReleasesJSONEnv] = `{"dar_01JABCDEF0123456789XYZ":{` +
				`"allowed_principal_ids":["33333333-3333-4333-8333-333333333333"],` +
				`"blob_name":"releases/example.dar","download_name":"example.txt"}}`
		},
		"empty allowlist": func(env map[string]string) {
			env[config.ReleasesJSONEnv] = `{"dar_01JABCDEF0123456789XYZ":{` +
				`"allowed_principal_ids":[],"blob_name":"releases/example.dar",` +
				`"download_name":"example.dar"}}`
		},
		"duplicate principal": func(env map[string]string) {
			env[config.ReleasesJSONEnv] = `{"dar_01JABCDEF0123456789XYZ":{` +
				`"allowed_principal_ids":["33333333-3333-4333-8333-333333333333",` +
				`"33333333-3333-4333-8333-333333333333"],` +
				`"blob_name":"releases/example.dar","download_name":"example.dar"}}`
		},
		"oversized policy": func(env map[string]string) {
			env[config.ReleasesJSONEnv] = `{"x":"` + strings.Repeat("a", 65*1024) + `"}`
		},
	}

	for name, mutate := range tests {
		name, mutate := name, mutate
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			env := validEnvironment()
			mutate(env)
			if _, err := config.ParseEnvironment(env); err == nil {
				t.Fatal("ParseEnvironment() error = nil, want rejection")
			}
		})
	}
}

func TestParseEnvironmentRejectsDuplicateBlobAndDownloadNames(t *testing.T) {
	t.Parallel()

	base := validEnvironment()
	base[config.ReleasesJSONEnv] = `{
      "dar_01JABCDEF0123456789XYZ": {
        "allowed_principal_ids": ["33333333-3333-4333-8333-333333333333"],
        "blob_name": "releases/example.dar",
        "download_name": "example.dar"
      },
      "dar_01JABCDEF0123456789XYA": {
        "allowed_principal_ids": ["44444444-4444-4444-8444-444444444444"],
        "blob_name": "releases/example.dar",
        "download_name": "example.dar"
      }
    }`

	if _, err := config.ParseEnvironment(base); err == nil {
		t.Fatal("ParseEnvironment() error = nil, want duplicate rejection")
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
	if cfg.Port != 8081 || cfg.ReleaseCount() != 1 {
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
				`"allowed_principal_ids":["33333333-3333-4333-8333-333333333333"],` +
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
		`"allowed_principal_ids":["33333333-3333-4333-8333-333333333333"],` +
		`"blob_name":"releases/example.dar","blob_name":"releases/other.dar",` +
		`"download_name":"example.dar"}}`
	if _, err := config.ParseEnvironment(environment); err == nil {
		t.Fatal("accepted duplicate JSON key")
	}
}
