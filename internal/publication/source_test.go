package publication

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverSourceSelectsHighestCanonicalProductRelease(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeReleaseZIP(t, root, "v8.31.1.01", zipEntry{name: "product.dar", body: []byte("old")})
	writeReleaseZIP(
		t, root, "v8.31.1.02",
		zipEntry{name: "product.dar", body: []byte("new")},
		zipEntry{name: "guide.pdf", body: []byte("guide")},
	)

	release, err := DiscoverSource(root, "dar_distribution_01")
	if err != nil {
		t.Fatalf("DiscoverSource() error = %v", err)
	}
	if release.Version != "v8.31.1.02" || release.DownloadName != "dar-8.31.1.02.zip" ||
		release.DARPath != "releases/v8.31.1.02/dar-8.31.1.02.zip" ||
		len(release.DARSHA256) != 64 || release.DARSize < 1 {
		t.Fatalf("release = %#v", release)
	}
}

func TestDiscoverSourceRejectsUnsafeOrAmbiguousZIP(t *testing.T) {
	t.Parallel()
	for name, entries := range map[string][]zipEntry{
		"traversal":     {{name: "../product.dar", body: []byte("dar")}},
		"windows-drive": {{name: "C:/product.dar", body: []byte("dar")}},
		"two-dars": {
			{name: "one.dar", body: []byte("one")},
			{name: "two.dar", body: []byte("two")},
		},
		"unsupported": {
			{name: "product.dar", body: []byte("dar")},
			{name: "run.exe", body: []byte("no")},
		},
	} {
		name, entries := name, entries
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeReleaseZIP(t, root, "v8.31.1.01", entries...)
			if _, err := DiscoverSource(root, "dar_distribution_01"); err == nil {
				t.Fatal("DiscoverSource() accepted unsafe ZIP")
			}
		})
	}
}

func TestParseRecipientsCanonicalizesAndSortsProtectedInput(t *testing.T) {
	t.Parallel()
	recipients, err := ParseRecipients(bytes.NewBufferString(
		"first_name,last_name,email\n" +
			"Noah,Sample,NOAH@example.com\n" +
			"Ava,Example,ava@example.com\n",
	))
	if err != nil {
		t.Fatalf("ParseRecipients() error = %v", err)
	}
	if len(recipients) != 2 || recipients[0].Email != "ava@example.com" ||
		recipients[1].Email != "noah@example.com" {
		t.Fatalf("recipients = %#v", recipients)
	}
}

func TestParseRecipientsRejectsDuplicateAndNoncanonicalFiles(t *testing.T) {
	t.Parallel()
	inputs := []string{
		"first_name,last_name,email\nAva,Example,ava@example.com\nAva,Again,AVA@example.com\n",
		"first_name,last_name,email\r\nAva,Example,ava@example.com\r\n",
		"firstname,lastname,email\nAva,Example,ava@example.com\n",
	}
	for _, input := range inputs {
		if _, err := ParseRecipients(bytes.NewBufferString(input)); err == nil {
			t.Fatalf("ParseRecipients() accepted %q", input)
		}
	}
}

type zipEntry struct {
	name string
	body []byte
}

func writeReleaseZIP(t *testing.T, root, version string, entries ...zipEntry) {
	t.Helper()
	releasesDirectory := filepath.Join(root, "releases")
	if err := os.MkdirAll(releasesDirectory, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(releasesDirectory, "README.md"), []byte("release contract\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(root, "releases", version)
	if err := os.MkdirAll(directory, 0o750); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "dar-"+version[1:]+".zip")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	for _, entry := range entries {
		writer, createErr := archive.Create(entry.name)
		if createErr != nil {
			t.Fatal(createErr)
		}
		if _, writeErr := writer.Write(entry.body); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
