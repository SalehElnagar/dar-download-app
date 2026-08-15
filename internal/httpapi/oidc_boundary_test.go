package httpapi_test

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/SalehElnagar/dar-download-app/internal/config"
	"github.com/SalehElnagar/dar-download-app/internal/httpapi"
	"github.com/SalehElnagar/dar-download-app/internal/testsupport"
)

const (
	azureBoundaryTenant  = "11111111-1111-4111-8111-111111111111"
	azureBoundaryIssuer  = "https://login.microsoftonline.com/" + azureBoundaryTenant + "/v2.0"
	azureBoundarySubject = "6c0ed021-4271-4b5c-a975-341f56fc11ad"
)

func azureBoundaryHeaders(t *testing.T, tenantID, subject string) http.Header {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"auth_typ": "aad",
		"claims": []map[string]string{
			{"typ": "http://schemas.microsoft.com/identity/claims/tenantid", "val": tenantID},
			{"typ": "http://schemas.microsoft.com/identity/claims/objectidentifier", "val": subject},
		},
		"name_typ": "name",
		"role_typ": "role",
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	headers := make(http.Header)
	headers.Set("X-MS-CLIENT-PRINCIPAL", base64.StdEncoding.EncodeToString(payload))
	headers.Set("X-MS-CLIENT-PRINCIPAL-ID", subject)
	return headers
}

func newAzureBoundaryHandler(t *testing.T, storage *testsupport.Storage) http.Handler {
	t.Helper()
	cfg, err := config.ParseEnvironment(map[string]string{
		config.TrustedIdentityModeEnv:        string(config.TrustedIdentityModeAzureContainerApps),
		config.OIDCIssuerEnv:                 azureBoundaryIssuer,
		config.AzureContainerAppsTenantIDEnv: azureBoundaryTenant,
		config.StorageAccountNameEnv:         "stdardownloadpoc01",
		config.StorageContainerEnv:           "dar-releases",
		config.ManagedIdentityClientIDEnv:    managedIdentityID,
	})
	if err != nil {
		t.Fatalf("config.ParseEnvironment() error = %v", err)
	}
	return httpapi.New(cfg, storage, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

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
		"oversized subject": {
			headers: testsupport.OIDCHeaders(oidcIssuer, strings.Repeat("s", config.MaxOIDCSubjectBytes+1)),
			status:  http.StatusUnauthorized,
			code:    "authentication_required",
		},
		"platform principal mixed with valid generic identity": {
			headers: func() http.Header {
				headers := testsupport.OIDCHeaders(oidcIssuer, allowedSubject)
				headers.Set("X-MS-CLIENT-PRINCIPAL", "synthetic")
				return headers
			}(),
			status: http.StatusUnauthorized,
			code:   "authentication_required",
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
				downloadPath,
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

func TestAzureContainerAppsAcceptsAnyCanonicalIdentity(t *testing.T) {
	t.Parallel()

	for name, subject := range map[string]string{
		"first canonical object ID":     azureBoundarySubject,
		"different canonical object ID": "22222222-2222-4222-8222-222222222222",
	} {
		name, subject := name, subject
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			storage := &testsupport.Storage{Objects: map[string]testsupport.Object{
				blobName: {Data: []byte("dar"), ETag: `"etag-v1"`},
			}}
			recorder := request(
				t,
				newAzureBoundaryHandler(t, storage),
				http.MethodGet,
				downloadPath,
				azureBoundaryHeaders(t, azureBoundaryTenant, subject),
			)
			if recorder.Code != http.StatusOK || recorder.Body.String() != "dar" {
				t.Fatalf("response = %d %q", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestAzureContainerAppsIdentityMismatchesDenyBeforeStorage(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		headers http.Header
		status  int
		code    string
	}{
		"wrong tenant": {
			headers: azureBoundaryHeaders(t, "22222222-2222-4222-8222-222222222222", azureBoundarySubject),
			status:  http.StatusUnauthorized,
			code:    "authentication_required",
		},
		"invalid object ID": {
			headers: azureBoundaryHeaders(t, azureBoundaryTenant, "not-a-canonical-object-id"),
			status:  http.StatusUnauthorized,
			code:    "authentication_required",
		},
		"caller generic assertion": {
			headers: func() http.Header {
				headers := azureBoundaryHeaders(t, azureBoundaryTenant, azureBoundarySubject)
				headers.Set("X-DAR-OIDC-Issuer", azureBoundaryIssuer)
				headers.Set("X-DAR-OIDC-Subject", azureBoundarySubject)
				return headers
			}(),
			status: http.StatusUnauthorized,
			code:   "authentication_required",
		},
	}
	for name, testCase := range tests {
		name, testCase := name, testCase
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			storage := &testsupport.Storage{Objects: map[string]testsupport.Object{}}
			recorder := request(
				t,
				newAzureBoundaryHandler(t, storage),
				http.MethodGet,
				downloadPath,
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
