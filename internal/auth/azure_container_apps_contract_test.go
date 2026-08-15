package auth_test

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/SalehElnagar/dar-download-app/internal/auth"
	"github.com/SalehElnagar/dar-download-app/internal/config"
)

const (
	azureTenantID    = "11111111-1111-4111-8111-111111111111"
	azureIssuer      = "https://login.microsoftonline.com/" + azureTenantID + "/v2.0"
	azurePrincipalID = "6c0ed021-4271-4b5c-a975-341f56fc11ad"
)

func encodedAzurePrincipal(t *testing.T, payload map[string]any) string {
	t.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return base64.StdEncoding.EncodeToString(encoded)
}

func azurePrincipalPayload(claims []map[string]any) map[string]any {
	return map[string]any{
		"auth_typ": "aad",
		"claims":   claims,
		"name_typ": "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/name",
		"role_typ": "http://schemas.microsoft.com/ws/2008/06/identity/claims/role",
	}
}

func azurePrincipalHeaders(t *testing.T, payload map[string]any, principalID string) http.Header {
	t.Helper()
	headers := make(http.Header)
	headers.Set("X-MS-CLIENT-PRINCIPAL", encodedAzurePrincipal(t, payload))
	headers.Set("X-MS-CLIENT-PRINCIPAL-ID", principalID)
	return headers
}

func azurePrincipalHeadersFromJSON(payload string, principalID string) http.Header {
	headers := make(http.Header)
	headers.Set("X-MS-CLIENT-PRINCIPAL", base64.StdEncoding.EncodeToString([]byte(payload)))
	headers.Set("X-MS-CLIENT-PRINCIPAL-ID", principalID)
	return headers
}

func azureClaims(tenantType, objectType, tenantID, objectID string) []map[string]any {
	return []map[string]any{
		{"typ": tenantType, "val": tenantID},
		{"typ": objectType, "val": objectID},
		{"typ": "name", "val": "synthetic operator"},
	}
}

func authenticateAzure(headers http.Header) (auth.Identity, bool) {
	return auth.Authenticate(
		headers,
		azureIssuer,
		config.TrustedIdentityModeAzureContainerApps,
		azureTenantID,
	)
}

func TestAuthenticateAzureContainerAppsMapsOneVerifiedPrincipal(t *testing.T) {
	t.Parallel()

	claimNames := []struct {
		name       string
		tenantType string
		objectType string
	}{
		{
			name:       "mapped claim names",
			tenantType: "http://schemas.microsoft.com/identity/claims/tenantid",
			objectType: "http://schemas.microsoft.com/identity/claims/objectidentifier",
		},
		{name: "OIDC claim names", tenantType: "tid", objectType: "oid"},
	}
	for _, test := range claimNames {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			headers := azurePrincipalHeaders(
				t,
				azurePrincipalPayload(azureClaims(
					test.tenantType,
					test.objectType,
					azureTenantID,
					azurePrincipalID,
				)),
				azurePrincipalID,
			)
			headers.Set("X-MS-CLIENT-PRINCIPAL-IDP", "aad")
			identity, ok := authenticateAzure(headers)
			if !ok || identity.Issuer != azureIssuer || identity.Subject != azurePrincipalID {
				t.Fatalf("Authenticate() = %#v, %t", identity, ok)
			}
		})
	}
}

func TestAuthenticateAzureContainerAppsRejectsMalformedOrAmbiguousEvidence(t *testing.T) {
	t.Parallel()

	valid := func(t *testing.T) http.Header {
		return azurePrincipalHeaders(
			t,
			azurePrincipalPayload(azureClaims("tid", "oid", azureTenantID, azurePrincipalID)),
			azurePrincipalID,
		)
	}
	tests := map[string]func(*testing.T) http.Header{
		"missing principal": func(t *testing.T) http.Header {
			headers := valid(t)
			headers.Del("X-MS-CLIENT-PRINCIPAL")
			return headers
		},
		"missing principal ID": func(t *testing.T) http.Header {
			headers := valid(t)
			headers.Del("X-MS-CLIENT-PRINCIPAL-ID")
			return headers
		},
		"duplicate principal": func(t *testing.T) http.Header {
			headers := valid(t)
			headers.Add("X-MS-CLIENT-PRINCIPAL", headers.Get("X-MS-CLIENT-PRINCIPAL"))
			return headers
		},
		"duplicate principal ID": func(t *testing.T) http.Header {
			headers := valid(t)
			headers.Add("X-MS-CLIENT-PRINCIPAL-ID", azurePrincipalID)
			return headers
		},
		"invalid base64": func(t *testing.T) http.Header {
			headers := valid(t)
			headers.Set("X-MS-CLIENT-PRINCIPAL", "not base64")
			return headers
		},
		"oversized principal": func(t *testing.T) http.Header {
			headers := valid(t)
			headers.Set("X-MS-CLIENT-PRINCIPAL", strings.Repeat("A", 16*1024+1))
			return headers
		},
		"unknown top-level field": func(t *testing.T) http.Header {
			payload := azurePrincipalPayload(azureClaims("tid", "oid", azureTenantID, azurePrincipalID))
			payload["unexpected"] = true
			return azurePrincipalHeaders(t, payload, azurePrincipalID)
		},
		"case-variant top-level key": func(*testing.T) http.Header {
			return azurePrincipalHeadersFromJSON(
				`{"AUTH_TYP":"aad","claims":[{"typ":"tid","val":"`+azureTenantID+`"},{"typ":"oid","val":"`+azurePrincipalID+`"}],"name_typ":"name","role_typ":"role"}`,
				azurePrincipalID,
			)
		},
		"case-variant claim collision": func(*testing.T) http.Header {
			return azurePrincipalHeadersFromJSON(
				`{"auth_typ":"aad","claims":[{"typ":"ignored","TYP":"tid","val":"`+azureTenantID+`"},{"typ":"oid","val":"`+azurePrincipalID+`"}],"name_typ":"name","role_typ":"role"}`,
				azurePrincipalID,
			)
		},
		"wrong authentication type": func(t *testing.T) http.Header {
			payload := azurePrincipalPayload(azureClaims("tid", "oid", azureTenantID, azurePrincipalID))
			payload["auth_typ"] = "github"
			return azurePrincipalHeaders(t, payload, azurePrincipalID)
		},
		"missing tenant": func(t *testing.T) http.Header {
			return azurePrincipalHeaders(
				t,
				azurePrincipalPayload([]map[string]any{{"typ": "oid", "val": azurePrincipalID}}),
				azurePrincipalID,
			)
		},
		"duplicate tenant": func(t *testing.T) http.Header {
			claims := azureClaims("tid", "oid", azureTenantID, azurePrincipalID)
			claims = append(claims, map[string]any{"typ": "tid", "val": azureTenantID})
			return azurePrincipalHeaders(t, azurePrincipalPayload(claims), azurePrincipalID)
		},
		"wrong tenant": func(t *testing.T) http.Header {
			return azurePrincipalHeaders(
				t,
				azurePrincipalPayload(azureClaims(
					"tid",
					"oid",
					"22222222-2222-4222-8222-222222222222",
					azurePrincipalID,
				)),
				azurePrincipalID,
			)
		},
		"missing object ID": func(t *testing.T) http.Header {
			return azurePrincipalHeaders(
				t,
				azurePrincipalPayload([]map[string]any{{"typ": "tid", "val": azureTenantID}}),
				azurePrincipalID,
			)
		},
		"duplicate object ID": func(t *testing.T) http.Header {
			claims := azureClaims("tid", "oid", azureTenantID, azurePrincipalID)
			claims = append(claims, map[string]any{"typ": "oid", "val": azurePrincipalID})
			return azurePrincipalHeaders(t, azurePrincipalPayload(claims), azurePrincipalID)
		},
		"mismatched object ID": func(t *testing.T) http.Header {
			return azurePrincipalHeaders(
				t,
				azurePrincipalPayload(azureClaims(
					"tid",
					"oid",
					azureTenantID,
					"22222222-2222-4222-8222-222222222222",
				)),
				azurePrincipalID,
			)
		},
		"noncanonical principal ID": func(t *testing.T) http.Header {
			headers := valid(t)
			headers.Set("X-MS-CLIENT-PRINCIPAL-ID", strings.ToUpper(azurePrincipalID))
			return headers
		},
		"claim with extra field": func(t *testing.T) http.Header {
			claims := azureClaims("tid", "oid", azureTenantID, azurePrincipalID)
			claims[0]["unexpected"] = "value"
			return azurePrincipalHeaders(t, azurePrincipalPayload(claims), azurePrincipalID)
		},
		"too many claims": func(t *testing.T) http.Header {
			claims := azureClaims("tid", "oid", azureTenantID, azurePrincipalID)
			for index := 0; index < 62; index++ {
				claims = append(claims, map[string]any{"typ": "other", "val": "value"})
			}
			return azurePrincipalHeaders(t, azurePrincipalPayload(claims), azurePrincipalID)
		},
		"caller generic identity": func(t *testing.T) http.Header {
			headers := valid(t)
			headers.Set("X-DAR-OIDC-Issuer", azureIssuer)
			headers.Set("X-DAR-OIDC-Subject", azurePrincipalID)
			return headers
		},
		"duplicate IDP": func(t *testing.T) http.Header {
			headers := valid(t)
			headers.Add("X-MS-CLIENT-PRINCIPAL-IDP", "aad")
			headers.Add("X-MS-CLIENT-PRINCIPAL-IDP", "aad")
			return headers
		},
		"wrong IDP": func(t *testing.T) http.Header {
			headers := valid(t)
			headers.Set("X-MS-CLIENT-PRINCIPAL-IDP", "github")
			return headers
		},
	}
	for name, buildHeaders := range tests {
		name, buildHeaders := name, buildHeaders
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if identity, ok := authenticateAzure(buildHeaders(t)); ok {
				t.Fatalf("Authenticate() = %#v, true; want rejection", identity)
			}
		})
	}
}

func TestAuthenticateAzureContainerAppsRequiresMatchingIssuerAndTenantPolicy(t *testing.T) {
	t.Parallel()

	headers := azurePrincipalHeaders(
		t,
		azurePrincipalPayload(azureClaims("tid", "oid", azureTenantID, azurePrincipalID)),
		azurePrincipalID,
	)
	tests := []struct {
		issuer   string
		tenantID string
	}{
		{issuer: "https://login.microsoftonline.com/22222222-2222-4222-8222-222222222222/v2.0", tenantID: azureTenantID},
		{issuer: azureIssuer, tenantID: "22222222-2222-4222-8222-222222222222"},
		{issuer: "https://identity.example.com/" + azureTenantID, tenantID: azureTenantID},
	}
	for _, test := range tests {
		if identity, ok := auth.Authenticate(
			headers,
			test.issuer,
			config.TrustedIdentityModeAzureContainerApps,
			test.tenantID,
		); ok {
			t.Fatalf("Authenticate() = %#v, true for issuer %q and tenant %q", identity, test.issuer, test.tenantID)
		}
	}
}
