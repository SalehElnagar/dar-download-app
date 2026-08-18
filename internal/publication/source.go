// Package publication validates repository-owned DAR release inputs and publishes their
// immutable distribution contracts.
package publication

import (
	"archive/zip"
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	appconfig "github.com/SalehElnagar/dar-download-app/internal/config"
	"github.com/SalehElnagar/dar-download-app/internal/distribution"
)

const (
	maxArchiveBytes      = appconfig.MaxObjectSize
	maxArchiveEntries    = 32
	maxUncompressedBytes = uint64(1024 * 1024 * 1024)
	maxRecipientsBytes   = int64(1024 * 1024)
)

var (
	ErrSource             = errors.New("invalid release source")
	productVersionPattern = regexp.MustCompile(
		`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.([0-9]{2})$`,
	)
	releaseIDPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9_-]{0,62}[a-z0-9])?$`)
	namePattern      = regexp.MustCompile(`^[A-Za-z][A-Za-z .'-]{0,63}$`)
	emailPattern     = regexp.MustCompile(
		`^[a-z0-9.!#$%&'*+/=?^_` + "`" + `{|}~-]+@[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+$`,
	)
	windowsDrivePattern    = regexp.MustCompile(`^[A-Za-z]:`)
	allowedGuideExtensions = map[string]struct{}{
		".docx": {}, ".md": {}, ".pdf": {}, ".txt": {},
	}
)

// SourceRelease is the pipeline-derived release intent. Field order is canonical JSON order.
type SourceRelease struct {
	DARPath       string `json:"dar_path"`
	DARSHA256     string `json:"dar_sha256"`
	DARSize       int64  `json:"dar_size"`
	DownloadName  string `json:"download_name"`
	ReleaseID     string `json:"release_id"`
	SchemaVersion string `json:"schema_version"`
	Version       string `json:"version"`
}

// DiscoverSource selects the highest canonical product release and validates its ZIP fully.
func DiscoverSource(repositoryRoot, releaseID string) (SourceRelease, error) {
	if !releaseIDPattern.MatchString(releaseID) {
		return SourceRelease{}, ErrSource
	}
	root, err := cleanDirectory(repositoryRoot)
	if err != nil {
		return SourceRelease{}, err
	}
	releases := filepath.Join(root, "releases")
	if err := rejectSymlink(releases); err != nil {
		return SourceRelease{}, ErrSource
	}
	entries, err := os.ReadDir(releases)
	if err != nil || len(entries) == 0 {
		return SourceRelease{}, ErrSource
	}
	type candidate struct {
		version string
		key     [4]uint64
	}
	candidates := make([]candidate, 0, len(entries))
	for _, entry := range entries {
		if entry.Name() == "README.md" && entry.Type().IsRegular() {
			continue
		}
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return SourceRelease{}, ErrSource
		}
		key, valid := productVersionKey(entry.Name())
		if !valid {
			return SourceRelease{}, ErrSource
		}
		candidates = append(candidates, candidate{version: entry.Name(), key: key})
	}
	if len(candidates) == 0 {
		return SourceRelease{}, ErrSource
	}
	sort.Slice(candidates, func(left, right int) bool {
		return versionKeyLess(candidates[left].key, candidates[right].key)
	})
	version := candidates[len(candidates)-1].version
	downloadName := "dar-" + strings.TrimPrefix(version, "v") + ".zip"
	releaseDirectory := filepath.Join(releases, version)
	releaseEntries, err := os.ReadDir(releaseDirectory)
	if err != nil || len(releaseEntries) != 1 || releaseEntries[0].Name() != downloadName ||
		releaseEntries[0].IsDir() || releaseEntries[0].Type()&os.ModeSymlink != 0 {
		return SourceRelease{}, ErrSource
	}
	archivePath := filepath.Join(releaseDirectory, downloadName)
	archiveInfo, err := os.Stat(archivePath)
	if err != nil || archiveInfo.Size() < 1 || archiveInfo.Size() > maxArchiveBytes {
		return SourceRelease{}, ErrSource
	}
	if err := validateReleaseZIP(archivePath); err != nil {
		return SourceRelease{}, err
	}
	digest, err := fileDigest(archivePath)
	if err != nil {
		return SourceRelease{}, err
	}
	return SourceRelease{
		DARPath:   filepath.ToSlash(filepath.Join("releases", version, downloadName)),
		DARSHA256: digest, DARSize: archiveInfo.Size(), DownloadName: downloadName,
		ReleaseID: releaseID, SchemaVersion: "1.0", Version: version,
	}, nil
}

// ParseRecipients reads the protected pipeline file into a bounded canonical cohort.
func ParseRecipients(reader io.Reader) ([]distribution.Recipient, error) {
	if reader == nil {
		return nil, ErrSource
	}
	raw, err := readBounded(reader, maxRecipientsBytes)
	if err != nil || len(raw) < 1 || int64(len(raw)) > maxRecipientsBytes ||
		raw[len(raw)-1] != '\n' || strings.ContainsRune(string(raw), '\r') {
		return nil, ErrSource
	}
	for _, character := range raw {
		if character > 0x7f {
			return nil, ErrSource
		}
	}
	parser := csv.NewReader(strings.NewReader(string(raw)))
	parser.FieldsPerRecord = -1
	parser.ReuseRecord = false
	rows, err := parser.ReadAll()
	if err != nil || len(rows) < 2 || len(rows[0]) != 3 ||
		rows[0][0] != "first_name" || rows[0][1] != "last_name" || rows[0][2] != "email" {
		return nil, ErrSource
	}
	recipients := make([]distribution.Recipient, 0, len(rows)-1)
	seen := make(map[string]struct{}, len(rows)-1)
	for _, row := range rows[1:] {
		if len(row) != 3 {
			return nil, ErrSource
		}
		firstName := strings.TrimSpace(row[0])
		lastName := strings.TrimSpace(row[1])
		email := strings.ToLower(strings.TrimSpace(row[2]))
		if !namePattern.MatchString(firstName) || !namePattern.MatchString(lastName) ||
			len(email) > 254 || !emailPattern.MatchString(email) {
			return nil, ErrSource
		}
		if _, duplicate := seen[email]; duplicate {
			return nil, ErrSource
		}
		seen[email] = struct{}{}
		recipients = append(recipients, distribution.Recipient{
			Email: email, FirstName: firstName, LastName: lastName,
		})
	}
	sort.Slice(recipients, func(left, right int) bool {
		if recipients[left].Email != recipients[right].Email {
			return recipients[left].Email < recipients[right].Email
		}
		if recipients[left].FirstName != recipients[right].FirstName {
			return recipients[left].FirstName < recipients[right].FirstName
		}
		return recipients[left].LastName < recipients[right].LastName
	})
	return recipients, nil
}

func cleanDirectory(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", ErrSource
	}
	root, err := filepath.Abs(raw)
	if err != nil {
		return "", ErrSource
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", ErrSource
	}
	directoryInfo, err := os.Stat(root)
	if err != nil || !directoryInfo.IsDir() {
		return "", ErrSource
	}
	return filepath.Clean(root), nil
}

func rejectSymlink(name string) error {
	directoryInfo, err := os.Lstat(name)
	if err != nil || !directoryInfo.IsDir() || directoryInfo.Mode()&os.ModeSymlink != 0 {
		return ErrSource
	}
	return nil
}

func productVersionKey(version string) ([4]uint64, bool) {
	matches := productVersionPattern.FindStringSubmatch(version)
	if len(matches) != 5 {
		return [4]uint64{}, false
	}
	var key [4]uint64
	for index := range key {
		value, err := strconv.ParseUint(matches[index+1], 10, 32)
		if err != nil {
			return [4]uint64{}, false
		}
		key[index] = value
	}
	return key, true
}

func versionKeyLess(left, right [4]uint64) bool {
	for index := range left {
		if left[index] != right[index] {
			return left[index] < right[index]
		}
	}
	return false
}

func validateReleaseZIP(archivePath string) error {
	archive, err := zip.OpenReader(archivePath)
	if err != nil {
		return ErrSource
	}
	defer archive.Close()
	if len(archive.File) < 1 || len(archive.File) > maxArchiveEntries {
		return ErrSource
	}
	seen := make(map[string]struct{}, len(archive.File))
	var total uint64
	darCount := 0
	for _, member := range archive.File {
		if err := validateMemberName(member.Name); err != nil || member.Flags&0x1 != 0 ||
			member.Method != zip.Store && member.Method != zip.Deflate ||
			member.Mode()&os.ModeSymlink != 0 {
			return ErrSource
		}
		canonicalName := strings.ToLower(member.Name)
		if _, duplicate := seen[canonicalName]; duplicate {
			return ErrSource
		}
		seen[canonicalName] = struct{}{}
		if member.FileInfo().IsDir() {
			continue
		}
		if member.UncompressedSize64 > maxUncompressedBytes-total {
			return ErrSource
		}
		total += member.UncompressedSize64
		extension := strings.ToLower(path.Ext(member.Name))
		if extension == ".dar" {
			if member.UncompressedSize64 < 1 {
				return ErrSource
			}
			darCount++
		} else if _, allowed := allowedGuideExtensions[extension]; !allowed {
			return ErrSource
		}
		stream, openErr := member.Open()
		if openErr != nil {
			return ErrSource
		}
		if uint64(member.UncompressedSize) != member.UncompressedSize64 {
			_ = stream.Close()
			return ErrSource
		}
		memberSize := int64(member.UncompressedSize)
		written, copyErr := io.Copy(io.Discard, io.LimitReader(stream, memberSize+1))
		closeErr := stream.Close()
		if copyErr != nil || closeErr != nil || written != memberSize {
			return ErrSource
		}
	}
	if darCount != 1 {
		return ErrSource
	}
	return nil
}

func validateMemberName(name string) error {
	if name == "" || len([]byte(name)) > 512 || strings.HasPrefix(name, "/") ||
		strings.Contains(name, "\\") || filepath.VolumeName(name) != "" ||
		windowsDrivePattern.MatchString(name) {
		return ErrSource
	}
	for _, character := range name {
		if character < 0x20 || character == 0x7f {
			return ErrSource
		}
	}
	cleaned := path.Clean(name)
	if cleaned == "." || cleaned != strings.TrimSuffix(name, "/") {
		return ErrSource
	}
	for _, segment := range strings.Split(cleaned, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return ErrSource
		}
	}
	return nil
}

func fileDigest(name string) (string, error) {
	file, err := os.Open(filepath.Clean(name))
	if err != nil {
		return "", ErrSource
	}
	defer file.Close()
	digest := sha256.New()
	reader := bufio.NewReaderSize(file, 4*1024*1024)
	if _, err := io.Copy(digest, reader); err != nil {
		return "", ErrSource
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func readBounded(reader io.Reader, maximum int64) ([]byte, error) {
	if reader == nil || maximum < 1 {
		return nil, ErrSource
	}
	var buffer bytes.Buffer
	limited := &io.LimitedReader{R: reader, N: maximum + 1}
	if _, err := buffer.ReadFrom(limited); err != nil || int64(buffer.Len()) > maximum {
		return nil, ErrSource
	}
	return buffer.Bytes(), nil
}

func verifySourceArtifact(repositoryRoot string, release SourceRelease) (string, error) {
	if release.SchemaVersion != "1.0" || !releaseIDPattern.MatchString(release.ReleaseID) ||
		!productVersionPattern.MatchString(release.Version) ||
		release.DownloadName != "dar-"+strings.TrimPrefix(release.Version, "v")+".zip" ||
		release.DARPath != "releases/"+release.Version+"/"+release.DownloadName ||
		len(release.DARSHA256) != 64 || release.DARSize < 1 || release.DARSize > maxArchiveBytes {
		return "", ErrSource
	}
	root, err := cleanDirectory(repositoryRoot)
	if err != nil {
		return "", err
	}
	name := filepath.Join(root, filepath.FromSlash(release.DARPath))
	resolved, err := filepath.EvalSymlinks(name)
	if err != nil || filepath.Clean(resolved) != filepath.Clean(name) {
		return "", ErrSource
	}
	relative, err := filepath.Rel(root, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", ErrSource
	}
	artifactInfo, err := os.Stat(resolved)
	if err != nil || !artifactInfo.Mode().IsRegular() || artifactInfo.Size() != release.DARSize {
		return "", ErrSource
	}
	digest, err := fileDigest(resolved)
	if err != nil || digest != release.DARSHA256 {
		return "", ErrSource
	}
	return resolved, nil
}
