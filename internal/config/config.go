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
)

const (
	TrustedIdentityModeEnv         = "DAR_DOWNLOAD_TRUSTED_IDENTITY_MODE"
	OIDCIssuerEnv                  = "DAR_DOWNLOAD_OIDC_ISSUER"
	OIDCProviderNameEnv            = "DAR_DOWNLOAD_OIDC_PROVIDER_NAME"
	AzureContainerAppsTenantIDEnv  = "DAR_DOWNLOAD_AZURE_CONTAINER_APPS_TENANT_ID"
	StorageAccountNameEnv          = "DAR_DOWNLOAD_STORAGE_ACCOUNT_NAME"
	StorageContainerEnv            = "DAR_DOWNLOAD_STORAGE_CONTAINER"
	ManagedIdentityClientIDEnv     = "DAR_DOWNLOAD_MANAGED_IDENTITY_CLIENT_ID"
	PortEnv                        = "DAR_DOWNLOAD_PORT"
	obsoleteReleasesJSONEnv        = "DAR_DOWNLOAD_RELEASES_JSON"
	azureContainerAppsIssuerPrefix = "https://login.microsoftonline.com/"
	azureContainerAppsIssuerSuffix = "/v2.0"
	azureContainerAppsEntraIDP     = "aad"

	DefaultPort              = 8000
	MaxOIDCIssuerBytes       = 2048
	MaxOIDCProviderNameBytes = 32
	MaxOIDCSubjectBytes      = 255
	MaxObjectSize            = int64(256 * 1024 * 1024)
	MaxStorageSegment        = int64(4 * 1024 * 1024)
)

var (
	uuidPattern             = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	oidcProviderNamePattern = regexp.MustCompile(`^[A-Za-z0-9]+$`)
	storageAccountPattern   = regexp.MustCompile(`^[a-z0-9]{3,24}$`)
	containerPattern        = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$`)
)

// TrustedIdentityMode selects exactly one deployment-owned identity boundary.
type TrustedIdentityMode string

const (
	// TrustedIdentityModeOIDCHeaders accepts only the provider-neutral private-ingress headers.
	TrustedIdentityModeOIDCHeaders TrustedIdentityMode = "oidc_headers"
	// TrustedIdentityModeAzureContainerApps accepts only Container Apps platform principal headers.
	TrustedIdentityModeAzureContainerApps TrustedIdentityMode = "azure_container_apps"
	// TrustedIdentityModeAzureContainerAppsOIDC accepts only one custom-provider Container Apps principal.
	TrustedIdentityModeAzureContainerAppsOIDC TrustedIdentityMode = "azure_container_apps_oidc"
)

// ErrInvalid indicates that runtime policy is missing, ambiguous, or unsafe.
var ErrInvalid = errors.New("invalid DAR download configuration")

// Config is the immutable startup policy exposed to the application.
type Config struct {
	TrustedIdentityMode        TrustedIdentityMode
	OIDCIssuer                 string
	OIDCProviderName           string
	AzureContainerAppsTenantID string
	StorageAccountName         string
	StorageContainer           string
	ManagedIdentityClientID    string
	Port                       int
}

// IsCanonicalUUID reports whether value is one lowercase canonical UUID string.
func IsCanonicalUUID(value string) bool {
	return uuidPattern.MatchString(value)
}

// IsAzureContainerAppsIssuer binds the optional adapter to one exact global-Azure tenant issuer.
func IsAzureContainerAppsIssuer(value, tenantID string) bool {
	return IsCanonicalUUID(tenantID) &&
		value == azureContainerAppsIssuerPrefix+tenantID+azureContainerAppsIssuerSuffix
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

// IsValidOIDCProviderName reports whether value is one bounded Azure custom OIDC provider name.
func IsValidOIDCProviderName(value string) bool {
	return len(value) >= 1 && len(value) <= MaxOIDCProviderNameBytes &&
		oidcProviderNamePattern.MatchString(value) &&
		!strings.EqualFold(value, azureContainerAppsEntraIDP)
}

// LoadEnvironment reads only the documented environment variables.
func LoadEnvironment() (Config, error) {
	environment := make(map[string]string, 9)
	for _, name := range []string{
		TrustedIdentityModeEnv,
		OIDCIssuerEnv,
		OIDCProviderNameEnv,
		AzureContainerAppsTenantIDEnv,
		StorageAccountNameEnv,
		StorageContainerEnv,
		ManagedIdentityClientIDEnv,
		obsoleteReleasesJSONEnv,
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
	if _, exists := environment[obsoleteReleasesJSONEnv]; exists {
		return Config{}, ErrInvalid
	}
	required := []string{
		OIDCIssuerEnv,
		StorageAccountNameEnv,
		StorageContainerEnv,
		ManagedIdentityClientIDEnv,
	}
	for _, name := range required {
		if strings.TrimSpace(environment[name]) == "" {
			return Config{}, ErrInvalid
		}
	}
	if !IsValidOIDCIssuer(environment[OIDCIssuerEnv]) ||
		!IsCanonicalUUID(environment[ManagedIdentityClientIDEnv]) ||
		!storageAccountPattern.MatchString(environment[StorageAccountNameEnv]) ||
		!validContainerName(environment[StorageContainerEnv]) {
		return Config{}, ErrInvalid
	}

	mode := TrustedIdentityMode(environment[TrustedIdentityModeEnv])
	if mode == "" {
		mode = TrustedIdentityModeOIDCHeaders
	}
	azureTenantID, azureTenantPresent := environment[AzureContainerAppsTenantIDEnv]
	oidcProviderName, oidcProviderPresent := environment[OIDCProviderNameEnv]
	switch mode {
	case TrustedIdentityModeOIDCHeaders:
		if azureTenantID != "" || oidcProviderPresent {
			return Config{}, ErrInvalid
		}
	case TrustedIdentityModeAzureContainerApps:
		if oidcProviderPresent || !IsAzureContainerAppsIssuer(environment[OIDCIssuerEnv], azureTenantID) {
			return Config{}, ErrInvalid
		}
	case TrustedIdentityModeAzureContainerAppsOIDC:
		if azureTenantPresent || !oidcProviderPresent || !IsValidOIDCProviderName(oidcProviderName) {
			return Config{}, ErrInvalid
		}
	default:
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

	return Config{
		TrustedIdentityMode:        mode,
		OIDCIssuer:                 environment[OIDCIssuerEnv],
		OIDCProviderName:           oidcProviderName,
		AzureContainerAppsTenantID: azureTenantID,
		StorageAccountName:         environment[StorageAccountNameEnv],
		StorageContainer:           environment[StorageContainerEnv],
		ManagedIdentityClientID:    environment[ManagedIdentityClientIDEnv],
		Port:                       port,
	}, nil
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
