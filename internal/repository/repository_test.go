package repository_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

var (
	markdownLink = regexp.MustCompile(`\[[^]]+\]\(([^)]+)\)`)
	productZIP   = regexp.MustCompile(`^releases/v[0-9]+\.[0-9]+\.[0-9]+\.[0-9]{2}/dar-[0-9]+\.[0-9]+\.[0-9]+\.[0-9]{2}\.zip$`)
)

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve repository test source")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(source), "..", ".."))
}

func TestLocalMarkdownLinksResolve(t *testing.T) {
	root := repositoryRoot(t)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && isLocalOnlyDirectory(entry.Name()) {
			return filepath.SkipDir
		}
		if entry.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for _, match := range markdownLink.FindAllStringSubmatch(string(content), -1) {
			target := strings.SplitN(match[1], "#", 2)[0]
			if target == "" || strings.HasPrefix(target, "https://") || strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			if filepath.IsAbs(target) {
				t.Errorf("%s contains absolute local link %q", path, target)
				continue
			}
			resolved := filepath.Clean(filepath.Join(filepath.Dir(path), target))
			relative, relativeErr := filepath.Rel(root, resolved)
			if relativeErr != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				t.Errorf("%s contains out-of-repository link %q", path, target)
				continue
			}
			first := strings.Split(filepath.ToSlash(relative), "/")[0]
			if first == ".agents" || first == ".specify" || first == "specs" {
				t.Errorf("%s links to local-only path %q", path, target)
				continue
			}
			if _, statErr := os.Stat(resolved); statErr != nil {
				t.Errorf("%s contains unresolved local link %q: %v", path, target, statErr)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestProductionAutomationUsesAzureDevOpsOnly(t *testing.T) {
	root := repositoryRoot(t)
	workflowDir := filepath.Join(repositoryRoot(t), ".github", "workflows")
	if entries, err := os.ReadDir(workflowDir); err == nil && len(entries) != 0 {
		t.Fatalf("active GitHub Actions remain: %v", entries)
	} else if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	for _, pipeline := range []string{"ci.yml", "dar-release-distribution.yml"} {
		path := filepath.Join(root, "azure-pipelines", pipeline)
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(content)
		if !strings.Contains(text, "checkout: self") || strings.Contains(strings.ToLower(text), "sendgrid") {
			t.Errorf("%s violates the Azure DevOps production boundary", path)
		}
	}
}

func TestCanonicalContractDescribesBothApplicationRoutes(t *testing.T) {
	root := repositoryRoot(t)
	paths := []string{filepath.Join(root, "api", "openapi.yaml")}
	required := []string{
		"/healthz:",
		"/v1/releases/{version}/download/{file_name}:",
		"'200'",
		"'206'",
		"'401'",
		"'404'",
		"method_not_allowed",
		"'413'",
		"'416'",
		"'502'",
	}
	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := strings.ReplaceAll(string(content), `"`, `'`)
		for _, fragment := range required {
			if !strings.Contains(text, fragment) {
				t.Errorf("%s is missing contract fragment %q", path, fragment)
			}
		}
		for _, retired := range []string{
			"/v1/releases/{release_" + "id}/download:",
			"authorization_" + "denied",
		} {
			if strings.Contains(text, retired) {
				t.Errorf("%s contains retired contract fragment %q", path, retired)
			}
		}
	}
}

func TestRepositoryExcludesDeploymentStateAndAllowsOnlyCanonicalReleaseZIPs(t *testing.T) {
	root := repositoryRoot(t)
	forbiddenExtensions := map[string]bool{
		".bicep":  true,
		".dar":    true,
		".tf":     true,
		".tfvars": true,
	}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && isLocalOnlyDirectory(entry.Name()) {
			return filepath.SkipDir
		}
		if entry.IsDir() {
			return nil
		}
		relative, relativeErr := filepath.Rel(root, path)
		if relativeErr != nil {
			return relativeErr
		}
		if filepath.Ext(entry.Name()) == ".zip" && !productZIP.MatchString(filepath.ToSlash(relative)) {
			t.Errorf("noncanonical release ZIP: %s", path)
		}
		if forbiddenExtensions[filepath.Ext(entry.Name())] || strings.HasPrefix(entry.Name(), ".env") {
			t.Errorf("forbidden deployment, release, or environment artifact: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func isLocalOnlyDirectory(name string) bool {
	return name == ".git" || name == ".security" || name == ".agents" ||
		name == ".specify" || name == "specs"
}

func TestSecurityExceptionRegistryRemainsEmpty(t *testing.T) {
	path := filepath.Join(repositoryRoot(t), "security", "exceptions.yaml")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const emptyRegistry = `version: 1
policy:
  maximum_days: 90
  require_owner: true
  require_compensating_controls: true
  allow_wildcards: false
exceptions: []
`
	if string(content) != emptyRegistry {
		t.Fatal("automated security exceptions are unsupported; the registry must remain exactly empty")
	}
}
