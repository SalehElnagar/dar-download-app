// Package auth validates identity evidence from a trusted OIDC boundary.
package auth

import (
	"net/http"

	"github.com/SalehElnagar/dar-download-app/internal/config"
)

const (
	IssuerHeader  = "X-DAR-OIDC-Issuer"
	SubjectHeader = "X-DAR-OIDC-Subject"
)

// Identity is one exact issuer and subject pair that passed every boundary check.
type Identity struct {
	Issuer  string
	Subject string
}

// Authenticate returns one exact OIDC identity or false for ambiguous evidence.
func Authenticate(headers http.Header, expectedIssuer string) (Identity, bool) {
	if !config.IsValidOIDCIssuer(expectedIssuer) {
		return Identity{}, false
	}
	issuerValues := headers.Values(IssuerHeader)
	subjectValues := headers.Values(SubjectHeader)
	if len(issuerValues) != 1 || len(subjectValues) != 1 {
		return Identity{}, false
	}
	issuer := issuerValues[0]
	subject := subjectValues[0]
	if !config.IsValidOIDCIssuer(issuer) || issuer != expectedIssuer ||
		!config.IsValidOIDCSubject(subject) {
		return Identity{}, false
	}
	return Identity{Issuer: issuer, Subject: subject}, true
}
