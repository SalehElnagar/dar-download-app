package auth_test

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"regexp"
	"testing"

	"github.com/SalehElnagar/dar-download-app/internal/auth"
	"github.com/SalehElnagar/dar-download-app/internal/config"
)

func FuzzAuthenticateOIDCHeaders(f *testing.F) {
	const expectedIssuer = "https://identity.example.com/realms/customers/"
	f.Add(expectedIssuer, "customer:Case-Sensitive-001")
	f.Add("https://identity.example.com/realms/customers", "customer:Case-Sensitive-001")
	f.Add("http://identity.example.com", "customer:001")
	f.Add(expectedIssuer, "")

	f.Fuzz(func(t *testing.T, issuer, subject string) {
		headers := make(http.Header)
		headers.Set("X-DAR-OIDC-Issuer", issuer)
		headers.Set("X-DAR-OIDC-Subject", subject)
		identity, ok := auth.Authenticate(
			headers,
			expectedIssuer,
			config.TrustedIdentityModeOIDCHeaders,
			"",
		)
		expectedOK := issuer == expectedIssuer && config.IsValidOIDCSubject(subject)
		if ok != expectedOK {
			t.Fatalf("Authenticate() ok = %t, want %t for issuer %q and subject %q", ok, expectedOK, issuer, subject)
		}
		if ok && (identity.Issuer != expectedIssuer || identity.Subject != subject || issuer != expectedIssuer) {
			t.Fatalf("accepted inconsistent identity %#v", identity)
		}
	})
}

func FuzzAuthenticateAzureContainerApps(f *testing.F) {
	const (
		expectedTenant = "11111111-1111-4111-8111-111111111111"
		expectedIssuer = "https://login.microsoftonline.com/" + expectedTenant + "/v2.0"
		seedSubject    = "6c0ed021-4271-4b5c-a975-341f56fc11ad"
	)
	f.Add("aad", expectedTenant, seedSubject, seedSubject, false, false, false)
	f.Add("aad", "22222222-2222-4222-8222-222222222222", seedSubject, seedSubject, false, false, false)
	f.Add("aad", expectedTenant, seedSubject, seedSubject, true, false, false)
	f.Add("aad", expectedTenant, seedSubject, seedSubject, false, false, true)

	canonicalUUID := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	f.Fuzz(func(
		t *testing.T,
		authType string,
		tenantClaim string,
		objectClaim string,
		principalID string,
		duplicateTenant bool,
		duplicateObject bool,
		addGenericHeaders bool,
	) {
		claims := []map[string]string{
			{"typ": "tid", "val": tenantClaim},
			{"typ": "oid", "val": objectClaim},
		}
		if duplicateTenant {
			claims = append(claims, map[string]string{"typ": "tid", "val": tenantClaim})
		}
		if duplicateObject {
			claims = append(claims, map[string]string{"typ": "oid", "val": objectClaim})
		}
		payload, err := json.Marshal(map[string]any{
			"auth_typ": authType,
			"claims":   claims,
			"name_typ": "name",
			"role_typ": "role",
		})
		if err != nil {
			t.Fatalf("json.Marshal() error = %v", err)
		}
		headers := make(http.Header)
		headers.Set("X-MS-CLIENT-PRINCIPAL", base64.StdEncoding.EncodeToString(payload))
		headers.Set("X-MS-CLIENT-PRINCIPAL-ID", principalID)
		if addGenericHeaders {
			headers.Set("X-DAR-OIDC-Issuer", expectedIssuer)
			headers.Set("X-DAR-OIDC-Subject", principalID)
		}

		identity, ok := auth.Authenticate(
			headers,
			expectedIssuer,
			config.TrustedIdentityModeAzureContainerApps,
			expectedTenant,
		)
		expectedOK := authType == "aad" && tenantClaim == expectedTenant &&
			canonicalUUID.MatchString(tenantClaim) &&
			objectClaim == principalID && canonicalUUID.MatchString(principalID) &&
			!duplicateTenant && !duplicateObject && !addGenericHeaders
		if ok != expectedOK {
			t.Fatalf("Authenticate() ok = %t, want %t", ok, expectedOK)
		}
		if ok && (identity.Issuer != expectedIssuer || identity.Subject != principalID) {
			t.Fatalf("accepted inconsistent identity %#v", identity)
		}
	})
}
