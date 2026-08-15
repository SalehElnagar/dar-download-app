package auth_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/SalehElnagar/dar-download-app/internal/auth"
	"github.com/SalehElnagar/dar-download-app/internal/config"
)

func oidcHeaders(issuer, subject string) http.Header {
	headers := make(http.Header)
	headers.Add("X-DAR-OIDC-Issuer", issuer)
	headers.Add("X-DAR-OIDC-Subject", subject)
	return headers
}

func TestAuthenticateAcceptsOneExactOIDCIdentity(t *testing.T) {
	t.Parallel()

	issuer := "https://identity.example.com/realms/customers/"
	subject := "customer:Case-Sensitive-001"
	identity, ok := auth.Authenticate(oidcHeaders(issuer, subject), issuer)
	if !ok || identity.Issuer != issuer || identity.Subject != subject {
		t.Fatalf("Authenticate() = %#v, %t", identity, ok)
	}
}

func TestAuthenticateRejectsAmbiguousOrUntrustedOIDCInputs(t *testing.T) {
	t.Parallel()

	issuer := "https://identity.example.com/realms/customers/"
	subject := "customer:Case-Sensitive-001"
	duplicateIssuer := oidcHeaders(issuer, subject)
	duplicateIssuer.Add("X-DAR-OIDC-Issuer", issuer)
	duplicateSubject := oidcHeaders(issuer, subject)
	duplicateSubject.Add("X-DAR-OIDC-Subject", subject)
	tests := map[string]http.Header{
		"missing both":         {},
		"missing issuer":       {http.CanonicalHeaderKey("X-DAR-OIDC-Subject"): {subject}},
		"missing subject":      {http.CanonicalHeaderKey("X-DAR-OIDC-Issuer"): {issuer}},
		"wrong issuer":         oidcHeaders(strings.TrimSuffix(issuer, "/"), subject),
		"different issuer":     oidcHeaders("https://other.example.com", subject),
		"empty subject":        oidcHeaders(issuer, ""),
		"control subject":      oidcHeaders(issuer, "customer\nforged"),
		"oversized subject":    oidcHeaders(issuer, strings.Repeat("s", config.MaxOIDCSubjectBytes+1)),
		"duplicate issuer":     duplicateIssuer,
		"duplicate subject":    duplicateSubject,
		"invalid expected URL": oidcHeaders(issuer, subject),
	}
	for name, headers := range tests {
		name, headers := name, headers
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			expected := issuer
			if name == "invalid expected URL" {
				expected = "http://identity.example.com"
			}
			if identity, ok := auth.Authenticate(headers, expected); ok {
				t.Fatalf("Authenticate() = %#v, true; want rejection", identity)
			}
		})
	}
}

func TestProviderSpecificIdentityHeadersHaveNoAlias(t *testing.T) {
	t.Parallel()

	legacyHeader := "X-" + "MS-CLIENT-PRINCIPAL"
	headers := make(http.Header)
	headers.Set(legacyHeader, "synthetic")
	headers.Set(legacyHeader+"-ID", "customer:legacy")
	if identity, ok := auth.Authenticate(headers, "https://identity.example.com"); ok {
		t.Fatalf("Authenticate() = %#v, true; want rejection", identity)
	}
}
