package auth_test

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/SalehElnagar/dar-download-app/internal/auth"
	"github.com/SalehElnagar/dar-download-app/internal/config"
)

func FuzzAuthenticateAzureContainerAppsOIDC(f *testing.F) {
	f.Add(customACASubject, customACAProvider, customACAProvider, false)
	f.Add("subject", "aad", "aad", false)
	f.Add("subject", customACAProvider, customACAProvider, true)
	f.Fuzz(func(t *testing.T, subject, authenticationType, idp string, mixedGenericEvidence bool) {
		payload, err := json.Marshal(customPrincipalPayload(
			authenticationType,
			[]map[string]any{{"typ": "sub", "val": "bounded"}},
		))
		if err != nil {
			t.Fatal(err)
		}
		headers := make(http.Header)
		headers.Set("X-MS-CLIENT-PRINCIPAL", base64.StdEncoding.EncodeToString(payload))
		headers.Set("X-MS-CLIENT-PRINCIPAL-ID", subject)
		headers.Set("X-MS-CLIENT-PRINCIPAL-IDP", idp)
		if mixedGenericEvidence {
			headers.Set(auth.IssuerHeader, customACAIssuer)
			headers.Set(auth.SubjectHeader, subject)
		}

		identity, ok := auth.Authenticate(
			headers,
			auth.BoundaryPolicy{
				ExpectedIssuer:           customACAIssuer,
				Mode:                     config.TrustedIdentityModeAzureContainerAppsOIDC,
				ExpectedOIDCProviderName: customACAProvider,
			},
		)
		expectedOK := config.IsValidOIDCSubject(subject) &&
			authenticationType == customACAProvider &&
			idp == customACAProvider &&
			!mixedGenericEvidence
		if ok != expectedOK {
			t.Fatalf("Authenticate() ok = %t, want %t", ok, expectedOK)
		}
		if ok && (identity.Issuer != customACAIssuer || identity.Subject != subject) {
			t.Fatalf("Authenticate() identity = %#v", identity)
		}
	})
}
