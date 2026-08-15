package auth

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/SalehElnagar/dar-download-app/internal/config"
	"github.com/SalehElnagar/dar-download-app/internal/strictjson"
)

const (
	azurePrincipalHeader         = "X-MS-CLIENT-PRINCIPAL"
	azurePrincipalIDHeader       = "X-MS-CLIENT-PRINCIPAL-ID"
	azurePrincipalIDPHeader      = "X-MS-CLIENT-PRINCIPAL-IDP"
	azurePrincipalHeaderPrefix   = "X-MS-CLIENT-PRINCIPAL"
	azureAuthenticationType      = "aad"
	maxAzurePrincipalHeaderBytes = 16 * 1024
	maxAzurePrincipalClaims      = 64
	maxAzureClaimTypeBytes       = 512
	maxAzureClaimValueBytes      = 4096
	mappedAzureObjectIDClaim     = "http://schemas.microsoft.com/identity/claims/objectidentifier"
	mappedAzureTenantIDClaim     = "http://schemas.microsoft.com/identity/claims/tenantid"
	standardAzureObjectIDClaim   = "oid"
	standardAzureTenantIDClaim   = "tid"
)

type azurePrincipal struct {
	AuthenticationType string       `json:"auth_typ"`
	Claims             []azureClaim `json:"claims"`
	NameType           string       `json:"name_typ"`
	RoleType           string       `json:"role_typ"`
}

type azureClaim struct {
	Type  string `json:"typ"`
	Value string `json:"val"`
}

func authenticateAzureContainerApps(
	headers http.Header,
	expectedIssuer string,
	expectedTenantID string,
) (Identity, bool) {
	if !config.IsAzureContainerAppsIssuer(expectedIssuer, expectedTenantID) {
		return Identity{}, false
	}
	principalValues := headers.Values(azurePrincipalHeader)
	principalIDValues := headers.Values(azurePrincipalIDHeader)
	if len(principalValues) != 1 || len(principalIDValues) != 1 {
		return Identity{}, false
	}
	if idpValues := headers.Values(azurePrincipalIDPHeader); len(idpValues) > 1 ||
		(len(idpValues) == 1 && idpValues[0] != azureAuthenticationType) {
		return Identity{}, false
	}
	principalID := principalIDValues[0]
	if !config.IsCanonicalUUID(principalID) {
		return Identity{}, false
	}
	encodedPrincipal := principalValues[0]
	if encodedPrincipal == "" || len(encodedPrincipal) > maxAzurePrincipalHeaderBytes {
		return Identity{}, false
	}
	decodedPrincipal, err := base64.StdEncoding.Strict().DecodeString(encodedPrincipal)
	if err != nil {
		return Identity{}, false
	}
	if !hasExactAzurePrincipalSchema(decodedPrincipal) {
		return Identity{}, false
	}
	var principal azurePrincipal
	if err := strictjson.Decode(decodedPrincipal, maxAzurePrincipalHeaderBytes, &principal); err != nil ||
		principal.AuthenticationType != azureAuthenticationType ||
		len(principal.Claims) < 1 || len(principal.Claims) > maxAzurePrincipalClaims {
		return Identity{}, false
	}

	tenantID := ""
	objectID := ""
	tenantClaims := 0
	objectClaims := 0
	for _, claim := range principal.Claims {
		if !validAzureClaimText(claim.Type, maxAzureClaimTypeBytes) ||
			!validAzureClaimText(claim.Value, maxAzureClaimValueBytes) {
			return Identity{}, false
		}
		switch claim.Type {
		case standardAzureTenantIDClaim, mappedAzureTenantIDClaim:
			tenantClaims++
			tenantID = claim.Value
		case standardAzureObjectIDClaim, mappedAzureObjectIDClaim:
			objectClaims++
			objectID = claim.Value
		}
	}
	if tenantClaims != 1 || objectClaims != 1 ||
		!config.IsCanonicalUUID(tenantID) || tenantID != expectedTenantID ||
		!config.IsCanonicalUUID(objectID) || objectID != principalID {
		return Identity{}, false
	}
	return Identity{Issuer: expectedIssuer, Subject: principalID}, true
}

func hasExactAzurePrincipalSchema(raw []byte) bool {
	var principalFields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &principalFields); err != nil ||
		!hasExactJSONFields(principalFields, "auth_typ", "claims", "name_typ", "role_typ") {
		return false
	}

	var claims []map[string]json.RawMessage
	if err := json.Unmarshal(principalFields["claims"], &claims); err != nil {
		return false
	}
	for _, claimFields := range claims {
		if !hasExactJSONFields(claimFields, "typ", "val") {
			return false
		}
	}
	return true
}

func hasExactJSONFields(fields map[string]json.RawMessage, names ...string) bool {
	if len(fields) != len(names) {
		return false
	}
	for _, name := range names {
		if _, exists := fields[name]; !exists {
			return false
		}
	}
	return true
}

func hasAzureContainerAppsIdentityHeaders(headers http.Header) bool {
	for name := range headers {
		if strings.HasPrefix(strings.ToUpper(name), azurePrincipalHeaderPrefix) {
			return true
		}
	}
	return false
}

func validAzureClaimText(value string, maximumBytes int) bool {
	if value == "" || len(value) > maximumBytes || !utf8.ValidString(value) {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}
