package auth_test

import (
	"net/http"
	"testing"

	"github.com/SalehElnagar/dar-download-app/internal/auth"
	"github.com/SalehElnagar/dar-download-app/internal/testsupport"
)

func FuzzAuthenticatePrincipalHeader(f *testing.F) {
	valid := testsupport.EasyAuthHeaders(principalID, tenantID)
	f.Add(valid.Get("X-Ms-Client-Principal"), principalID)
	f.Add("%%%", principalID)
	f.Add("", "")

	f.Fuzz(func(t *testing.T, encoded, assertedPrincipal string) {
		headers := http.Header{
			"X-Ms-Client-Principal":    []string{encoded},
			"X-Ms-Client-Principal-Id": []string{assertedPrincipal},
		}
		principal, ok := auth.Authenticate(headers, tenantID)
		if ok && (principal.ID != assertedPrincipal || principal.TenantID != tenantID) {
			t.Fatalf("accepted inconsistent principal %#v", principal)
		}
	})
}
