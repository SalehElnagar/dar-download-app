package main

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunValidateReportsBoundedSourceEvidenceWithoutPII(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	releaseDirectory := filepath.Join(root, "releases", "v8.31.1.01")
	if err := os.MkdirAll(releaseDirectory, 0o750); err != nil {
		t.Fatal(err)
	}
	archiveFile, err := os.Create(filepath.Join(releaseDirectory, "dar-8.31.1.01.zip"))
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(archiveFile)
	member, err := archive.Create("product.dar")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := member.Write([]byte("dar")); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := archiveFile.Close(); err != nil {
		t.Fatal(err)
	}
	recipients := filepath.Join(t.TempDir(), "notification-recipients.csv")
	if err := os.WriteFile(
		recipients,
		[]byte("first_name,last_name,email\nAva,Example,ava@example.com\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := run([]string{
		"validate", "--repository-root", root, "--release-id", "dar_distribution_01",
		"--recipients-file", recipients,
	}, nil, &stdout, &stderr)

	if exitCode != 0 || stderr.Len() != 0 ||
		!strings.Contains(stdout.String(), `"release_version":"v8.31.1.01"`) ||
		!strings.Contains(stdout.String(), `"recipient_count":1`) ||
		strings.Contains(stdout.String(), "@") {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
}

func TestRunRejectsUnknownCommandWithoutDumpingEnvironment(t *testing.T) {
	t.Parallel()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	environment := map[string]string{"DAR_PUBLISHER_SENDGRID_API_KEY": "must-not-print"}

	exitCode := run([]string{"unknown"}, environment, &stdout, &stderr)

	if exitCode == 0 || strings.Contains(stdout.String()+stderr.String(), "must-not-print") {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
}
