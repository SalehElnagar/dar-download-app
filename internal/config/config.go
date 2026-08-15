// Package config parses the complete fail-closed runtime policy.
package config

import (
	"errors"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/SalehElnagar/dar-download-app/internal/strictjson"
)

const (
	OIDCIssuerEnv              = "DAR_DOWNLOAD_OIDC_ISSUER"
	StorageAccountNameEnv      = "DAR_DOWNLOAD_STORAGE_ACCOUNT_NAME"
	StorageContainerEnv        = "DAR_DOWNLOAD_STORAGE_CONTAINER"
	ManagedIdentityClientIDEnv = "DAR_DOWNLOAD_MANAGED_IDENTITY_CLIENT_ID"
	ReleasesJSONEnv            = "DAR_DOWNLOAD_RELEASES_JSON"
	PortEnv                    = "DAR_DOWNLOAD_PORT"

	DefaultPort         = 8000
	MaxReleases         = 32
	MaxPolicyBytes      = 64 * 1024
	MaxBlobNameBytes    = 1024
	MaxOIDCIssuerBytes  = 2048
	MaxOIDCSubjectBytes = 255
	MaxObjectSize       = int64(256 * 1024 * 1024)
	MaxStorageSegment   = int64(4 * 1024 * 1024)
)

var (
	uuidPattern           = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	releaseIDPattern      = regexp.MustCompile(`^dar_[A-Za-z0-9_-]{16,96}$`)
	storageAccountPattern = regexp.MustCompile(`^[a-z0-9]{3,24}$`)
	containerPattern      = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$`)
	downloadNamePattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,122}[.]dar$`)
)

// ErrInvalid indicates that runtime policy is missing, ambiguous, or unsafe.
var ErrInvalid = errors.New("invalid DAR download configuration")

// Config is the immutable startup policy exposed to the application.
type Config struct {
	OIDCIssuer              string
	StorageAccountName      string
	StorageContainer        string
	ManagedIdentityClientID string
	Port                    int
	releases                map[string]ReleasePolicy
}

// ReleasePolicy binds one opaque route to one Blob, filename, and subject set.
type ReleasePolicy struct {
	ID              string
	BlobName        string
	DownloadName    string
	allowedSubjects map[string]struct{}
}

// Allows reports whether the exact case-sensitive subject is entitled to the release.
func (release ReleasePolicy) Allows(subject string) bool {
	_, ok := release.allowedSubjects[subject]
	return ok
}

// Release resolves one opaque identifier without accepting a caller-controlled Blob path.
func (cfg Config) Release(releaseID string) (ReleasePolicy, bool) {
	release, ok := cfg.releases[releaseID]
	return release, ok
}

// ReleaseCount returns the number of startup-validated release policies.
func (cfg Config) ReleaseCount() int {
	return len(cfg.releases)
}

// IsReleaseID reports whether a route identifier has the exact opaque format.
func IsReleaseID(value string) bool {
	return releaseIDPattern.MatchString(value)
}

func isCanonicalUUID(value string) bool {
	return uuidPattern.MatchString(value)
}

// IsValidOIDCIssuer reports whether value is one exact bounded HTTPS issuer URL.
func IsValidOIDCIssuer(value string) bool {
	if !validBoundedText(value, MaxOIDCIssuerBytes) {
		return false
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.Hostname() == "" ||
		parsed.User != nil || parsed.Opaque != "" || parsed.RawQuery != "" || parsed.ForceQuery ||
		parsed.Fragment != "" || parsed.RawFragment != "" || parsed.String() != value {
		return false
	}
	return validControlFree(parsed.Path)
}

// IsValidOIDCSubject reports whether value is one bounded opaque OIDC subject.
func IsValidOIDCSubject(value string) bool {
	return validBoundedText(value, MaxOIDCSubjectBytes)
}

// LoadEnvironment reads only the documented environment variables.
func LoadEnvironment() (Config, error) {
	environment := make(map[string]string, 6)
	for _, name := range []string{
		OIDCIssuerEnv,
		StorageAccountNameEnv,
		StorageContainerEnv,
		ManagedIdentityClientIDEnv,
		ReleasesJSONEnv,
		PortEnv,
	} {
		if value, ok := os.LookupEnv(name); ok {
			environment[name] = value
		}
	}
	return ParseEnvironment(environment)
}

// ParseEnvironment constructs a complete policy or returns ErrInvalid.
func ParseEnvironment(environment map[string]string) (Config, error) {
	required := []string{
		OIDCIssuerEnv,
		StorageAccountNameEnv,
		StorageContainerEnv,
		ManagedIdentityClientIDEnv,
		ReleasesJSONEnv,
	}
	for _, name := range required {
		if strings.TrimSpace(environment[name]) == "" {
			return Config{}, ErrInvalid
		}
	}
	if !IsValidOIDCIssuer(environment[OIDCIssuerEnv]) ||
		!isCanonicalUUID(environment[ManagedIdentityClientIDEnv]) ||
		!storageAccountPattern.MatchString(environment[StorageAccountNameEnv]) ||
		!validContainerName(environment[StorageContainerEnv]) {
		return Config{}, ErrInvalid
	}

	port := DefaultPort
	if rawPort, exists := environment[PortEnv]; exists {
		parsed, err := strconv.Atoi(rawPort)
		if err != nil || parsed < 1 || parsed > 65535 {
			return Config{}, ErrInvalid
		}
		port = parsed
	}

	releases, err := parseReleases(environment[ReleasesJSONEnv])
	if err != nil {
		return Config{}, ErrInvalid
	}
	return Config{
		OIDCIssuer:              environment[OIDCIssuerEnv],
		StorageAccountName:      environment[StorageAccountNameEnv],
		StorageContainer:        environment[StorageContainerEnv],
		ManagedIdentityClientID: environment[ManagedIdentityClientIDEnv],
		Port:                    port,
		releases:                releases,
	}, nil
}

type releaseDocument struct {
	AllowedSubjects []string `json:"allowed_subjects"`
	BlobName        string   `json:"blob_name"`
	DownloadName    string   `json:"download_name"`
}

func parseReleases(raw string) (map[string]ReleasePolicy, error) {
	var documents map[string]releaseDocument
	if err := strictjson.Decode([]byte(raw), MaxPolicyBytes, &documents); err != nil {
		return nil, ErrInvalid
	}
	if len(documents) < 1 || len(documents) > MaxReleases {
		return nil, ErrInvalid
	}

	releases := make(map[string]ReleasePolicy, len(documents))
	seenBlobs := make(map[string]struct{}, len(documents))
	seenDownloads := make(map[string]struct{}, len(documents))
	for releaseID, document := range documents {
		if !IsReleaseID(releaseID) ||
			!validBlobName(document.BlobName) ||
			!downloadNamePattern.MatchString(document.DownloadName) ||
			len(document.AllowedSubjects) == 0 {
			return nil, ErrInvalid
		}
		if _, exists := seenBlobs[document.BlobName]; exists {
			return nil, ErrInvalid
		}
		if _, exists := seenDownloads[document.DownloadName]; exists {
			return nil, ErrInvalid
		}
		seenBlobs[document.BlobName] = struct{}{}
		seenDownloads[document.DownloadName] = struct{}{}

		allowedSubjects := make(map[string]struct{}, len(document.AllowedSubjects))
		for _, subject := range document.AllowedSubjects {
			if !IsValidOIDCSubject(subject) {
				return nil, ErrInvalid
			}
			if _, duplicate := allowedSubjects[subject]; duplicate {
				return nil, ErrInvalid
			}
			allowedSubjects[subject] = struct{}{}
		}
		releases[releaseID] = ReleasePolicy{
			ID:              releaseID,
			BlobName:        document.BlobName,
			DownloadName:    document.DownloadName,
			allowedSubjects: allowedSubjects,
		}
	}
	return releases, nil
}

func validBoundedText(value string, maximumBytes int) bool {
	return value != "" && len(value) <= maximumBytes && utf8.ValidString(value) && validControlFree(value)
}

func validControlFree(value string) bool {
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func validContainerName(value string) bool {
	if !containerPattern.MatchString(value) {
		return false
	}
	return !strings.Contains(value, "--")
}

func validBlobName(value string) bool {
	if value == "" || !utf8.ValidString(value) || len([]byte(value)) > MaxBlobNameBytes {
		return false
	}
	if strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") ||
		strings.ContainsAny(value, `\?#`) {
		return false
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}
