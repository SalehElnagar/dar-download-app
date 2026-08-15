package auth_test

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/SalehElnagar/dar-download-app/internal/auth"
)

const (
	tenantID    = "11111111-1111-4111-8111-111111111111"
	principalID = "33333333-3333-4333-8333-333333333333"
)

type claim struct {
	Type  string `json:"typ"`
	Value string `json:"val"`
}

func identityHeaders(t *testing.T, principal, tenant string, extraClaims ...claim) http.Header {
	t.Helper()
	payload := map[string]any{
		"auth_typ": "aad",
		"claims": append([]claim{
			{Type: "http://schemas.microsoft.com/identity/claims/objectidentifier", Value: principal},
			{Type: "http://schemas.microsoft.com/identity/claims/tenantid", Value: tenant},
		}, extraClaims...),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return http.Header{
		"X-Ms-Client-Principal":    []string{base64.StdEncoding.EncodeToString(raw)},
		"X-Ms-Client-Principal-Id": []string{principal},
	}
}

func TestAuthenticateAcceptsOneExactAADPrincipal(t *testing.T) {
	t.Parallel()

	principal, ok := auth.Authenticate(identityHeaders(t, principalID, tenantID), tenantID)
	if !ok {
		t.Fatal("Authenticate() ok = false, want true")
	}
	if principal.ID != principalID || principal.TenantID != tenantID {
		t.Fatalf("principal = %#v", principal)
	}
}

func TestAuthenticateRejectsUntrustedOrAmbiguousEvidence(t *testing.T) {
	t.Parallel()

	tests := map[string]func(*testing.T) http.Header{
		"missing headers": func(*testing.T) http.Header { return http.Header{} },
		"wrong tenant": func(t *testing.T) http.Header {
			return identityHeaders(t, principalID, "44444444-4444-4444-8444-444444444444")
		},
		"principal header mismatch": func(t *testing.T) http.Header {
			headers := identityHeaders(t, principalID, tenantID)
			headers.Set("X-Ms-Client-Principal-Id", "55555555-5555-4555-8555-555555555555")
			return headers
		},
		"non canonical principal": func(t *testing.T) http.Header {
			return identityHeaders(t, "AAAAAAAA-3333-4333-8333-333333333333", tenantID)
		},
		"invalid base64": func(*testing.T) http.Header {
			return http.Header{
				"X-Ms-Client-Principal":    []string{"%%%"},
				"X-Ms-Client-Principal-Id": []string{principalID},
			}
		},
		"oversized payload": func(*testing.T) http.Header {
			return http.Header{
				"X-Ms-Client-Principal":    []string{strings.Repeat("A", 16*1024+1)},
				"X-Ms-Client-Principal-Id": []string{principalID},
			}
		},
		"conflicting object id": func(t *testing.T) http.Header {
			return identityHeaders(t, principalID, tenantID, claim{
				Type:  "oid",
				Value: "66666666-6666-4666-8666-666666666666",
			})
		},
		"too many claims": func(t *testing.T) http.Header {
			extra := make([]claim, 63)
			for index := range extra {
				extra[index] = claim{Type: "name", Value: "synthetic"}
			}
			return identityHeaders(t, principalID, tenantID, extra...)
		},
		"multiple principal headers": func(t *testing.T) http.Header {
			headers := identityHeaders(t, principalID, tenantID)
			headers["X-Ms-Client-Principal"] = append(
				headers["X-Ms-Client-Principal"],
				headers.Get("X-Ms-Client-Principal"),
			)
			return headers
		},
	}

	for name, headers := range tests {
		name, headers := name, headers
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if principal, ok := auth.Authenticate(headers(t), tenantID); ok {
				t.Fatalf("Authenticate() = %#v, true; want rejection", principal)
			}
		})
	}
}

func TestAuthenticateRejectsUnexpectedClaimShape(t *testing.T) {
	t.Parallel()

	payload := `{"auth_typ":"aad","claims":[` +
		`{"typ":"oid","val":"` + principalID + `","extra":"bad"},` +
		`{"typ":"tid","val":"` + tenantID + `"}]}`
	headers := http.Header{
		"X-Ms-Client-Principal":    []string{base64.StdEncoding.EncodeToString([]byte(payload))},
		"X-Ms-Client-Principal-Id": []string{principalID},
	}
	if _, ok := auth.Authenticate(headers, tenantID); ok {
		t.Fatal("Authenticate() ok = true, want false")
	}
}
