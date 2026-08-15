package testsupport

import (
	"net/http"

	"github.com/SalehElnagar/dar-download-app/internal/auth"
)

// OIDCHeaders constructs synthetic evidence from the trusted upstream boundary.
func OIDCHeaders(issuer, subject string) http.Header {
	headers := make(http.Header)
	headers.Set(auth.IssuerHeader, issuer)
	headers.Set(auth.SubjectHeader, subject)
	return headers
}
