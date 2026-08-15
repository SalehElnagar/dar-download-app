package auth_test

import (
	"net/http"
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
		identity, ok := auth.Authenticate(headers, expectedIssuer)
		expectedOK := issuer == expectedIssuer && config.IsValidOIDCSubject(subject)
		if ok != expectedOK {
			t.Fatalf("Authenticate() ok = %t, want %t for issuer %q and subject %q", ok, expectedOK, issuer, subject)
		}
		if ok && (identity.Issuer != expectedIssuer || identity.Subject != subject || issuer != expectedIssuer) {
			t.Fatalf("accepted inconsistent identity %#v", identity)
		}
	})
}
