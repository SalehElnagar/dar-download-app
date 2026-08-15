package httpapi_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/SalehElnagar/dar-download-app/internal/testsupport"
)

func TestOIDCIdentityMismatchesDenyBeforeStorage(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		headers http.Header
		status  int
		code    string
	}{
		"issuer trailing slash": {
			headers: testsupport.OIDCHeaders(strings.TrimSuffix(oidcIssuer, "/"), allowedSubject),
			status:  http.StatusUnauthorized,
			code:    "authentication_required",
		},
		"subject case": {
			headers: testsupport.OIDCHeaders(oidcIssuer, strings.ToLower(allowedSubject)),
			status:  http.StatusForbidden,
			code:    "authorization_denied",
		},
	}
	for name, testCase := range tests {
		name, testCase := name, testCase
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			storage := &testsupport.Storage{Objects: map[string]testsupport.Object{}}
			recorder := request(
				t,
				newHandler(t, storage),
				http.MethodGet,
				"/v1/releases/"+releaseID+"/download",
				testCase.headers,
			)
			assertJSONError(t, recorder, testCase.status, testCase.code)
			statCalls, openCalls, _ := storage.Counts()
			if len(statCalls) != 0 || len(openCalls) != 0 {
				t.Fatalf("storage calls = %#v %#v", statCalls, openCalls)
			}
		})
	}
}
