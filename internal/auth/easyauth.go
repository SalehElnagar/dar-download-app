// Package auth validates trusted Azure Container Apps Authentication evidence.
package auth

import (
	"encoding/base64"
	"net/http"
	"strings"

	"github.com/SalehElnagar/dar-download-app/internal/config"
	"github.com/SalehElnagar/dar-download-app/internal/strictjson"
)

const (
	principalHeader   = "X-Ms-Client-Principal"
	principalIDHeader = "X-Ms-Client-Principal-Id"
	maxEncodedBytes   = 16 * 1024
	maxClaims         = 64
)

var (
	objectIDClaimTypes = map[string]struct{}{
		"oid": {},
		"http://schemas.microsoft.com/identity/claims/objectidentifier": {},
	}
	tenantIDClaimTypes = map[string]struct{}{
		"tid": {},
		"http://schemas.microsoft.com/identity/claims/tenantid": {},
	}
)

// Principal is identity evidence that passed every application-level check.
type Principal struct {
	ID       string
	TenantID string
}

type principalDocument struct {
	AuthType string  `json:"auth_typ"`
	Claims   []claim `json:"claims"`
	NameType string  `json:"name_typ,omitempty"`
	RoleType string  `json:"role_typ,omitempty"`
}

type claim struct {
	Type  string `json:"typ"`
	Value string `json:"val"`
}

// Authenticate returns one exact principal or false for any ambiguous evidence.
func Authenticate(headers http.Header, expectedTenantID string) (Principal, bool) {
	if !config.IsCanonicalUUID(expectedTenantID) {
		return Principal{}, false
	}
	encodedValues := headers.Values(principalHeader)
	principalValues := headers.Values(principalIDHeader)
	if len(encodedValues) != 1 || len(principalValues) != 1 ||
		!config.IsCanonicalUUID(principalValues[0]) ||
		len(encodedValues[0]) == 0 || len(encodedValues[0]) > maxEncodedBytes {
		return Principal{}, false
	}

	decoded, err := base64.StdEncoding.Strict().DecodeString(encodedValues[0])
	if err != nil || len(decoded) == 0 || len(decoded) > maxEncodedBytes {
		return Principal{}, false
	}
	var document principalDocument
	if err := strictjson.Decode(decoded, maxEncodedBytes, &document); err != nil ||
		!strings.EqualFold(document.AuthType, "aad") ||
		len(document.Claims) == 0 || len(document.Claims) > maxClaims {
		return Principal{}, false
	}

	objectIDs := make(map[string]struct{})
	tenantIDs := make(map[string]struct{})
	for _, item := range document.Claims {
		claimType := strings.ToLower(item.Type)
		if _, relevant := objectIDClaimTypes[claimType]; relevant {
			if !config.IsCanonicalUUID(item.Value) {
				return Principal{}, false
			}
			objectIDs[item.Value] = struct{}{}
		}
		if _, relevant := tenantIDClaimTypes[claimType]; relevant {
			if !config.IsCanonicalUUID(item.Value) {
				return Principal{}, false
			}
			tenantIDs[item.Value] = struct{}{}
		}
	}
	if len(objectIDs) != 1 || len(tenantIDs) != 1 {
		return Principal{}, false
	}
	if _, matchesPrincipal := objectIDs[principalValues[0]]; !matchesPrincipal {
		return Principal{}, false
	}
	if _, matchesTenant := tenantIDs[expectedTenantID]; !matchesTenant {
		return Principal{}, false
	}
	return Principal{ID: principalValues[0], TenantID: expectedTenantID}, true
}
