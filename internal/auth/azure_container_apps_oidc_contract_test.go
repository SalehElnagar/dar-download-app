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
	customACAIssuer   = "https://identity.example.com/realms/customers/"
	customACAProvider = "DuendePOC"
	customACASubject  = "Duende|Case-Sensitive:42"
)

func customPrincipalPayload(authenticationType string, claims []map[string]any) map[string]any {
	return map[string]any{
		"auth_typ": authenticationType,
		"claims":   claims,
		"name_typ": "name",
		"role_typ": "role",
	}
}

func encodedCustomPrincipal(t *testing.T, payload map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

func customPrincipalHeaders(t *testing.T, providerName, principalID string) http.Header {
	t.Helper()
	headers := make(http.Header)
	headers.Set("X-MS-CLIENT-PRINCIPAL", encodedCustomPrincipal(t, customPrincipalPayload(
		providerName,
		[]map[string]any{{"typ": "sub", "val": "claim-value-is-not-authoritative"}},
	)))
	headers.Set("X-MS-CLIENT-PRINCIPAL-ID", principalID)
	headers.Set("X-MS-CLIENT-PRINCIPAL-IDP", providerName)
	return headers
}

func customPrincipalHeadersFromJSON(payload, providerName, principalID string) http.Header {
	headers := make(http.Header)
	headers.Set("X-MS-CLIENT-PRINCIPAL", base64.StdEncoding.EncodeToString([]byte(payload)))
	headers.Set("X-MS-CLIENT-PRINCIPAL-ID", principalID)
	headers.Set("X-MS-CLIENT-PRINCIPAL-IDP", providerName)
	return headers
}

func authenticateCustomACA(headers http.Header) (auth.Identity, bool) {
	return auth.Authenticate(
		headers,
		auth.BoundaryPolicy{
			ExpectedIssuer:           customACAIssuer,
			Mode:                     config.TrustedIdentityModeAzureContainerAppsOIDC,
			ExpectedOIDCProviderName: customACAProvider,
		},
	)
}

func TestAuthenticateCustomACAOIDCMapsProtectedPrincipalID(t *testing.T) {
	t.Parallel()

	headers := customPrincipalHeaders(t, customACAProvider, customACASubject)
	headers.Set("X-MS-CLIENT-PRINCIPAL-NAME", "display-name-is-not-identity")
	identity, ok := authenticateCustomACA(headers)
	if !ok || identity.Issuer != customACAIssuer || identity.Subject != customACASubject {
		t.Fatalf("Authenticate() = %#v, %t", identity, ok)
	}
}

func TestAuthenticateCustomACAOIDCRejectsMalformedOrAmbiguousEvidence(t *testing.T) {
	t.Parallel()

	valid := func(t *testing.T) http.Header {
		return customPrincipalHeaders(t, customACAProvider, customACASubject)
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
		"missing IDP": func(t *testing.T) http.Header {
			headers := valid(t)
			headers.Del("X-MS-CLIENT-PRINCIPAL-IDP")
			return headers
		},
		"duplicate principal": func(t *testing.T) http.Header {
			headers := valid(t)
			headers.Add("X-MS-CLIENT-PRINCIPAL", headers.Get("X-MS-CLIENT-PRINCIPAL"))
			return headers
		},
		"duplicate principal ID": func(t *testing.T) http.Header {
			headers := valid(t)
			headers.Add("X-MS-CLIENT-PRINCIPAL-ID", customACASubject)
			return headers
		},
		"duplicate IDP": func(t *testing.T) http.Header {
			headers := valid(t)
			headers.Add("X-MS-CLIENT-PRINCIPAL-IDP", customACAProvider)
			return headers
		},
		"invalid base64": func(t *testing.T) http.Header {
			headers := valid(t)
			headers.Set("X-MS-CLIENT-PRINCIPAL", "not base64")
			return headers
		},
		"base64 with carriage return": func(t *testing.T) http.Header {
			headers := valid(t)
			encoded := headers.Get("X-MS-CLIENT-PRINCIPAL")
			headers.Set("X-MS-CLIENT-PRINCIPAL", encoded[:4]+"\r"+encoded[4:])
			return headers
		},
		"base64 with line feed": func(t *testing.T) http.Header {
			headers := valid(t)
			encoded := headers.Get("X-MS-CLIENT-PRINCIPAL")
			headers.Set("X-MS-CLIENT-PRINCIPAL", encoded[:4]+"\n"+encoded[4:])
			return headers
		},
		"unpadded base64": func(t *testing.T) http.Header {
			headers := valid(t)
			for length := 1; length <= 3; length++ {
				payload := customPrincipalPayload(
					customACAProvider,
					[]map[string]any{{"typ": "sub", "val": strings.Repeat("v", length)}},
				)
				encoded := encodedCustomPrincipal(t, payload)
				if strings.HasSuffix(encoded, "=") {
					headers.Set("X-MS-CLIENT-PRINCIPAL", strings.TrimRight(encoded, "="))
					return headers
				}
			}
			t.Fatal("unable to construct padded Base64 fixture")
			return nil
		},
		"oversized principal": func(t *testing.T) http.Header {
			headers := valid(t)
			headers.Set("X-MS-CLIENT-PRINCIPAL", strings.Repeat("A", 16*1024+1))
			return headers
		},
		"unknown top-level field": func(t *testing.T) http.Header {
			payload := customPrincipalPayload(customACAProvider, []map[string]any{{"typ": "sub", "val": "value"}})
			payload["unexpected"] = true
			headers := valid(t)
			headers.Set("X-MS-CLIENT-PRINCIPAL", encodedCustomPrincipal(t, payload))
			return headers
		},
		"missing top-level field": func(*testing.T) http.Header {
			return customPrincipalHeadersFromJSON(
				`{"auth_typ":"DuendePOC","claims":[{"typ":"sub","val":"value"}],"name_typ":"name"}`,
				customACAProvider,
				customACASubject,
			)
		},
		"case-variant top-level key": func(*testing.T) http.Header {
			return customPrincipalHeadersFromJSON(
				`{"AUTH_TYP":"DuendePOC","claims":[{"typ":"sub","val":"value"}],"name_typ":"name","role_typ":"role"}`,
				customACAProvider,
				customACASubject,
			)
		},
		"duplicate JSON key": func(*testing.T) http.Header {
			return customPrincipalHeadersFromJSON(
				`{"auth_typ":"DuendePOC","auth_typ":"DuendePOC","claims":[{"typ":"sub","val":"value"}],"name_typ":"name","role_typ":"role"}`,
				customACAProvider,
				customACASubject,
			)
		},
		"claim with extra field": func(t *testing.T) http.Header {
			payload := customPrincipalPayload(customACAProvider, []map[string]any{{"typ": "sub", "val": "value", "extra": true}})
			headers := valid(t)
			headers.Set("X-MS-CLIENT-PRINCIPAL", encodedCustomPrincipal(t, payload))
			return headers
		},
		"claim missing value": func(*testing.T) http.Header {
			return customPrincipalHeadersFromJSON(
				`{"auth_typ":"DuendePOC","claims":[{"typ":"sub"}],"name_typ":"name","role_typ":"role"}`,
				customACAProvider,
				customACASubject,
			)
		},
		"case-variant claim key": func(*testing.T) http.Header {
			return customPrincipalHeadersFromJSON(
				`{"auth_typ":"DuendePOC","claims":[{"TYP":"sub","val":"value"}],"name_typ":"name","role_typ":"role"}`,
				customACAProvider,
				customACASubject,
			)
		},
		"empty claims": func(t *testing.T) http.Header {
			headers := valid(t)
			headers.Set("X-MS-CLIENT-PRINCIPAL", encodedCustomPrincipal(t, customPrincipalPayload(customACAProvider, nil)))
			return headers
		},
		"too many claims": func(t *testing.T) http.Header {
			claims := make([]map[string]any, 65)
			for index := range claims {
				claims[index] = map[string]any{"typ": "claim", "val": "value"}
			}
			headers := valid(t)
			headers.Set("X-MS-CLIENT-PRINCIPAL", encodedCustomPrincipal(t, customPrincipalPayload(customACAProvider, claims)))
			return headers
		},
		"oversized claim name": func(t *testing.T) http.Header {
			claims := []map[string]any{{"typ": strings.Repeat("t", 513), "val": "value"}}
			headers := valid(t)
			headers.Set("X-MS-CLIENT-PRINCIPAL", encodedCustomPrincipal(t, customPrincipalPayload(customACAProvider, claims)))
			return headers
		},
		"oversized claim value": func(t *testing.T) http.Header {
			claims := []map[string]any{{"typ": "claim", "val": strings.Repeat("v", 4097)}}
			headers := valid(t)
			headers.Set("X-MS-CLIENT-PRINCIPAL", encodedCustomPrincipal(t, customPrincipalPayload(customACAProvider, claims)))
			return headers
		},
		"control in claim": func(t *testing.T) http.Header {
			claims := []map[string]any{{"typ": "claim\nforged", "val": "value"}}
			headers := valid(t)
			headers.Set("X-MS-CLIENT-PRINCIPAL", encodedCustomPrincipal(t, customPrincipalPayload(customACAProvider, claims)))
			return headers
		},
		"empty subject": func(t *testing.T) http.Header {
			return customPrincipalHeaders(t, customACAProvider, "")
		},
		"oversized subject": func(t *testing.T) http.Header {
			return customPrincipalHeaders(t, customACAProvider, strings.Repeat("s", config.MaxOIDCSubjectBytes+1))
		},
		"control in subject": func(t *testing.T) http.Header {
			return customPrincipalHeaders(t, customACAProvider, "subject\nforged")
		},
		"wrong IDP": func(t *testing.T) http.Header {
			headers := valid(t)
			headers.Set("X-MS-CLIENT-PRINCIPAL-IDP", "OtherProvider")
			return headers
		},
		"Entra IDP and authentication type": func(t *testing.T) http.Header {
			headers := customPrincipalHeaders(t, "aad", customACASubject)
			return headers
		},
		"wrong authentication type": func(t *testing.T) http.Header {
			headers := valid(t)
			payload := customPrincipalPayload("OtherProvider", []map[string]any{{"typ": "sub", "val": "value"}})
			headers.Set("X-MS-CLIENT-PRINCIPAL", encodedCustomPrincipal(t, payload))
			return headers
		},
		"caller generic issuer": func(t *testing.T) http.Header {
			headers := valid(t)
			headers.Set(auth.IssuerHeader, customACAIssuer)
			return headers
		},
		"caller generic subject": func(t *testing.T) http.Header {
			headers := valid(t)
			headers.Set(auth.SubjectHeader, customACASubject)
			return headers
		},
		"caller generic identity pair": func(t *testing.T) http.Header {
			headers := valid(t)
			headers.Set(auth.IssuerHeader, customACAIssuer)
			headers.Set(auth.SubjectHeader, customACASubject)
			return headers
		},
	}
	for name, buildHeaders := range tests {
		name, buildHeaders := name, buildHeaders
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if identity, ok := authenticateCustomACA(buildHeaders(t)); ok {
				t.Fatalf("Authenticate() = %#v, true; want rejection", identity)
			}
		})
	}
}

func TestAuthenticateCustomACAOIDCRequiresExactDeploymentPolicy(t *testing.T) {
	t.Parallel()

	headers := customPrincipalHeaders(t, customACAProvider, customACASubject)
	tests := []struct {
		issuer       string
		tenantID     string
		providerName string
	}{
		{issuer: customACAIssuer, tenantID: azureTenantID, providerName: customACAProvider},
		{issuer: customACAIssuer, providerName: "duendepoc"},
		{issuer: customACAIssuer, providerName: "duende-poc"},
		{issuer: "http://identity.example.com", providerName: customACAProvider},
	}
	for _, test := range tests {
		if identity, ok := auth.Authenticate(
			headers,
			auth.BoundaryPolicy{
				ExpectedIssuer:           test.issuer,
				Mode:                     config.TrustedIdentityModeAzureContainerAppsOIDC,
				ExpectedAzureTenantID:    test.tenantID,
				ExpectedOIDCProviderName: test.providerName,
			},
		); ok {
			t.Fatalf("Authenticate() = %#v, true for %#v", identity, test)
		}
	}
}
