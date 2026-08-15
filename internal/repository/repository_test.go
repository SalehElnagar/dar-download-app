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
	actionUse    = regexp.MustCompile(`(?m)^\s*-?\s*uses:\s+([^@\s]+)@([^\s#]+)`)
	fullCommit   = regexp.MustCompile(`^[a-f0-9]{40}$`)
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

func TestWorkflowActionsUseFullCommitPins(t *testing.T) {
	workflowDir := filepath.Join(repositoryRoot(t), ".github", "workflows")
	entries, err := os.ReadDir(workflowDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || (filepath.Ext(entry.Name()) != ".yaml" && filepath.Ext(entry.Name()) != ".yml") {
			continue
		}
		path := filepath.Join(workflowDir, entry.Name())
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if strings.Contains(string(content), "pull_request_target:") {
			t.Errorf("%s uses the privileged pull_request_target trigger", path)
		}
		uses := actionUse.FindAllStringSubmatch(string(content), -1)
		if len(uses) == 0 {
			t.Errorf("%s has no externally pinned actions", path)
		}
		for _, use := range uses {
			if strings.HasPrefix(use[1], "./") {
				continue
			}
			if !fullCommit.MatchString(use[2]) {
				t.Errorf("%s action %s uses non-immutable ref %q", path, use[1], use[2])
			}
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

func TestRepositoryExcludesDeploymentAndReleaseArtifacts(t *testing.T) {
	root := repositoryRoot(t)
	forbiddenExtensions := map[string]bool{
		".bicep":  true,
		".dar":    true,
		".tf":     true,
		".tfvars": true,
		".zip":    true,
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
