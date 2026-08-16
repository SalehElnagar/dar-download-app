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
	containerAppsPrincipalHeader         = "X-MS-CLIENT-PRINCIPAL"
	containerAppsPrincipalIDHeader       = "X-MS-CLIENT-PRINCIPAL-ID"
	containerAppsPrincipalIDPHeader      = "X-MS-CLIENT-PRINCIPAL-IDP"
	containerAppsPrincipalHeaderPrefix   = "X-MS-CLIENT-PRINCIPAL"
	azureAuthenticationType              = "aad"
	maxContainerAppsPrincipalHeaderBytes = 16 * 1024
	maxContainerAppsPrincipalClaims      = 64
	maxContainerAppsClaimTypeBytes       = 512
	maxContainerAppsClaimValueBytes      = 4096
	mappedAzureObjectIDClaim             = "http://schemas.microsoft.com/identity/claims/objectidentifier"
	mappedAzureTenantIDClaim             = "http://schemas.microsoft.com/identity/claims/tenantid"
	standardAzureObjectIDClaim           = "oid"
	standardAzureTenantIDClaim           = "tid"
)

type containerAppsPrincipal struct {
	AuthenticationType string               `json:"auth_typ"`
	Claims             []containerAppsClaim `json:"claims"`
	NameType           string               `json:"name_typ"`
	RoleType           string               `json:"role_typ"`
}

type containerAppsClaim struct {
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
	principalValues := headers.Values(containerAppsPrincipalHeader)
	principalIDValues := headers.Values(containerAppsPrincipalIDHeader)
	if len(principalValues) != 1 || len(principalIDValues) != 1 {
		return Identity{}, false
	}
	if idpValues := headers.Values(containerAppsPrincipalIDPHeader); len(idpValues) > 1 ||
		(len(idpValues) == 1 && idpValues[0] != azureAuthenticationType) {
		return Identity{}, false
	}
	principalID := principalIDValues[0]
	if !config.IsCanonicalUUID(principalID) {
		return Identity{}, false
	}
	encodedPrincipal := principalValues[0]
	if encodedPrincipal == "" || len(encodedPrincipal) > maxContainerAppsPrincipalHeaderBytes {
		return Identity{}, false
	}
	decodedPrincipal, ok := decodeCanonicalContainerAppsPrincipal(encodedPrincipal)
	if !ok {
		return Identity{}, false
	}
	if !hasExactContainerAppsPrincipalSchema(decodedPrincipal) {
		return Identity{}, false
	}
	var principal containerAppsPrincipal
	if err := strictjson.Decode(decodedPrincipal, maxContainerAppsPrincipalHeaderBytes, &principal); err != nil ||
		principal.AuthenticationType != azureAuthenticationType ||
		len(principal.Claims) < 1 || len(principal.Claims) > maxContainerAppsPrincipalClaims {
		return Identity{}, false
	}

	tenantID := ""
	objectID := ""
	tenantClaims := 0
	objectClaims := 0
	for _, claim := range principal.Claims {
		if !validContainerAppsClaimText(claim.Type, maxContainerAppsClaimTypeBytes) ||
			!validContainerAppsClaimText(claim.Value, maxContainerAppsClaimValueBytes) {
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

func decodeCanonicalContainerAppsPrincipal(encodedPrincipal string) ([]byte, bool) {
	decodedPrincipal, err := base64.StdEncoding.Strict().DecodeString(encodedPrincipal)
	if err != nil || base64.StdEncoding.EncodeToString(decodedPrincipal) != encodedPrincipal {
		return nil, false
	}
	return decodedPrincipal, true
}

func hasExactContainerAppsPrincipalSchema(raw []byte) bool {
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
		if strings.HasPrefix(strings.ToUpper(name), containerAppsPrincipalHeaderPrefix) {
			return true
		}
	}
	return false
}

func validContainerAppsClaimText(value string, maximumBytes int) bool {
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
