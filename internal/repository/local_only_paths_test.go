package repository_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestTrackedTreeExcludesLocalOperationPaths(t *testing.T) {
	root := repositoryRoot(t)
	command := exec.Command("git", "ls-files", "-z")
	command.Dir = root
	output, err := command.Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	for _, path := range strings.Split(string(output), "\x00") {
		for _, prefix := range []string{".agents/", ".specify/", "specs/"} {
			if strings.HasPrefix(path, prefix) {
				t.Errorf("local-only operation path is tracked: %s", path)
			}
		}
	}

	ignore, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	lines := make(map[string]bool)
	for _, line := range strings.Split(string(ignore), "\n") {
		lines[line] = true
	}
	for _, rule := range []string{".agents/", ".specify/", "specs/"} {
		if !lines[rule] {
			t.Errorf(".gitignore is missing exact local-only rule %q", rule)
		}
	}

	dockerIgnore, err := os.ReadFile(filepath.Join(root, ".dockerignore"))
	if err != nil {
		t.Fatal(err)
	}
	dockerLines := make(map[string]bool)
	for _, line := range strings.Split(string(dockerIgnore), "\n") {
		dockerLines[line] = true
	}
	for _, rule := range []string{".agents", ".specify", "specs"} {
		if !dockerLines[rule] {
			t.Errorf(".dockerignore is missing exact local-only rule %q", rule)
		}
	}
}

func TestIgnoredLocalOperationFilesDoNotChangeSourceDigest(t *testing.T) {
	root := repositoryRoot(t)
	before := sourceDigest(t, root)

	for _, relative := range []string{".agents", ".specify", "specs"} {
		directory := filepath.Join(root, relative)
		_, statErr := os.Stat(directory)
		createdDirectory := os.IsNotExist(statErr)
		if statErr != nil && !createdDirectory {
			t.Fatal(statErr)
		}
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		probe, err := os.CreateTemp(directory, ".repository-ignore-probe-*")
		if err != nil {
			t.Fatal(err)
		}
		probePath := probe.Name()
		if closeErr := probe.Close(); closeErr != nil {
			t.Fatal(closeErr)
		}
		t.Cleanup(func() {
			_ = os.Remove(probePath)
			if createdDirectory {
				_ = os.Remove(directory)
			}
		})
	}

	if after := sourceDigest(t, root); after != before {
		t.Fatalf("ignored local operation files changed source digest: %s != %s", after, before)
	}
}

func sourceDigest(t *testing.T, root string) string {
	t.Helper()
	command := exec.Command(filepath.Join(root, "scripts", "source-digest.sh"))
	command.Dir = root
	output, err := command.Output()
	if err != nil {
		t.Fatalf("source digest: %v", err)
	}
	return strings.TrimSpace(string(output))
}

func TestTrackedContractsDescribeProviderNeutralBoundary(t *testing.T) {
	root := repositoryRoot(t)
	requiredByFile := map[string][]string{
		"api/openapi.yaml": {
			"X-DAR-OIDC-Issuer",
			"X-DAR-OIDC-Subject",
			"x-trusted-identity-boundary",
			"oidc_headers",
			"azure_container_apps",
			"never public credentials",
			"trusted OIDC-protected upstream endpoint",
			"/v1/releases/{version}/download/{file_name}",
			"subject-level authorization",
			`^bytes=(?:[0-9]+-[0-9]*|-[1-9][0-9]*)$`,
			"dar-download",
		},
		"docs/configuration.md": {
			"DAR_DOWNLOAD_TRUSTED_IDENTITY_MODE",
			"DAR_DOWNLOAD_OIDC_ISSUER",
			"DAR_DOWNLOAD_AZURE_CONTAINER_APPS_TENANT_ID",
			"X-DAR-OIDC-Issuer",
			"X-DAR-OIDC-Subject",
			"DAR_DOWNLOAD_RELEASES_JSON",
			"every successfully authenticated identity",
		},
		"docs/operations.md": {
			"/.auth/login/aad/callback",
		},
	}
	for relative, requiredFragments := range requiredByFile {
		content, err := os.ReadFile(filepath.Join(root, relative))
		if err != nil {
			t.Fatal(err)
		}
		text := strings.ToLower(string(content))
		for _, required := range requiredFragments {
			if !strings.Contains(text, strings.ToLower(required)) {
				t.Errorf("%s is missing %q", relative, required)
			}
		}
	}
}

func TestPublishedContractsExcludeRetiredStaticAuthorization(t *testing.T) {
	root := repositoryRoot(t)
	targets := []string{
		"README.md",
		"api/openapi.yaml",
		"docs/assurance-map.md",
		"docs/configuration.md",
		"docs/operations.md",
		"docs/threat-model.md",
	}
	for _, relative := range targets {
		content, err := os.ReadFile(filepath.Join(root, relative))
		if err != nil {
			t.Fatal(err)
		}
		for _, retired := range []string{
			"allowed_" + "subjects",
			"authorization_" + "denied",
			"/v1/releases/{release_" + "id}/download",
		} {
			if strings.Contains(string(content), retired) {
				t.Errorf("%s contains retired static authorization fragment %q", relative, retired)
			}
		}
	}
}

func TestTrackedTreeHasNoSupersededApplicationContract(t *testing.T) {
	root := repositoryRoot(t)
	command := exec.Command("git", "ls-files", "-z")
	command.Dir = root
	output, err := command.Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	forbidden := []*regexp.Regexp{
		regexp.MustCompile(`(?i)har` + `mony`),
		regexp.MustCompile(`(?i)easy` + `[[:space:]]+auth`),
		regexp.MustCompile(`(?i)allowed_` + `principal_ids`),
		regexp.MustCompile(`(?i)appservice` + `authsession`),
	}
	for _, relative := range strings.Split(string(output), "\x00") {
		if relative == "" {
			continue
		}
		content, readErr := os.ReadFile(filepath.Join(root, relative))
		if readErr != nil {
			t.Fatal(readErr)
		}
		for _, pattern := range forbidden {
			if pattern.Match(content) {
				t.Errorf("%s contains superseded application contract matching %q", relative, pattern)
			}
		}
	}
}

func TestProviderSpecificAdapterTermsStayInTheirOwnedBoundary(t *testing.T) {
	root := repositoryRoot(t)
	command := exec.Command("git", "ls-files", "-z")
	command.Dir = root
	output, err := command.Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	allowed := map[string]bool{
		"README.md":                             true,
		"api/openapi.yaml":                      true,
		"docs/assurance-map.md":                 true,
		"docs/configuration.md":                 true,
		"docs/operations.md":                    true,
		"docs/threat-model.md":                  true,
		"internal/auth/azure_container_apps.go": true,
		"internal/auth/azure_container_apps_contract_test.go": true,
		"internal/auth/oidc_fuzz_test.go":                     true,
		"internal/config/config.go":                           true,
		"internal/config/config_test.go":                      true,
		"internal/httpapi/oidc_boundary_test.go":              true,
		"internal/repository/local_only_paths_test.go":        true,
	}
	providerMarkers := []*regexp.Regexp{
		regexp.MustCompile(`(?i)(^|[^[:alnum:]_])en` + `tra([^[:alnum:]_]|$)`),
		regexp.MustCompile(`(?i)(^|[^[:alnum:]_])a` + `ad([^[:alnum:]_]|$)`),
		regexp.MustCompile(`(?i)micro` + `softonline`),
		regexp.MustCompile(`(?i)x-` + `ms-client`),
	}
	for _, relative := range strings.Split(string(output), "\x00") {
		if relative == "" || allowed[relative] {
			continue
		}
		content, readErr := os.ReadFile(filepath.Join(root, relative))
		if readErr != nil {
			t.Fatal(readErr)
		}
		for _, marker := range providerMarkers {
			if marker.Match(content) {
				t.Errorf("%s contains provider-adapter material outside the owned boundary", relative)
			}
		}
	}
}
