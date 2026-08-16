package httpapi_test

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/SalehElnagar/dar-download-app/internal/config"
	"github.com/SalehElnagar/dar-download-app/internal/httpapi"
	"github.com/SalehElnagar/dar-download-app/internal/testsupport"
)

const (
	customBoundaryProvider = "DuendePOC"
	customBoundarySubject  = "Duende|Case-Sensitive:42"
)

func customBoundaryHeaders(t *testing.T, providerName, subject string) http.Header {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"auth_typ": providerName,
		"claims": []map[string]string{
			{"typ": "sub", "val": "claim-value-is-not-authoritative"},
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
	headers.Set("X-MS-CLIENT-PRINCIPAL-IDP", providerName)
	return headers
}

func newCustomBoundaryHandler(t *testing.T, storage *testsupport.Storage) http.Handler {
	t.Helper()
	cfg, err := config.ParseEnvironment(map[string]string{
		config.TrustedIdentityModeEnv:     string(config.TrustedIdentityModeAzureContainerAppsOIDC),
		config.OIDCIssuerEnv:              oidcIssuer,
		config.OIDCProviderNameEnv:        customBoundaryProvider,
		config.StorageAccountNameEnv:      "stdardownloadpoc01",
		config.StorageContainerEnv:        "dar-releases",
		config.ManagedIdentityClientIDEnv: managedIdentityID,
	})
	if err != nil {
		t.Fatalf("config.ParseEnvironment() error = %v", err)
	}
	return httpapi.New(cfg, storage, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestCustomACAOIDCAllowsAnyProtectedOpaqueSubject(t *testing.T) {
	t.Parallel()

	storage := &testsupport.Storage{Objects: map[string]testsupport.Object{
		dynamicZIPBlob: {Data: []byte("zip"), ETag: `"zip-etag"`},
	}}
	recorder := request(
		t,
		newCustomBoundaryHandler(t, storage),
		http.MethodGet,
		dynamicRoute,
		customBoundaryHeaders(t, customBoundaryProvider, customBoundarySubject),
	)
	if recorder.Code != http.StatusOK || recorder.Body.String() != "zip" {
		t.Fatalf("response = %d %q", recorder.Code, recorder.Body.String())
	}
}

func TestCustomACAOIDCDeniesInvalidEvidenceBeforeStorage(t *testing.T) {
	t.Parallel()

	tests := map[string]func(*testing.T) http.Header{
		"missing IDP": func(t *testing.T) http.Header {
			headers := customBoundaryHeaders(t, customBoundaryProvider, customBoundarySubject)
			headers.Del("X-MS-CLIENT-PRINCIPAL-IDP")
			return headers
		},
		"wrong provider": func(t *testing.T) http.Header {
			return customBoundaryHeaders(t, "aad", customBoundarySubject)
		},
		"caller generic evidence": func(t *testing.T) http.Header {
			headers := customBoundaryHeaders(t, customBoundaryProvider, customBoundarySubject)
			headers.Set("X-DAR-OIDC-Issuer", oidcIssuer)
			headers.Set("X-DAR-OIDC-Subject", customBoundarySubject)
			return headers
		},
	}
	for name, buildHeaders := range tests {
		name, buildHeaders := name, buildHeaders
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			storage := &testsupport.Storage{Objects: map[string]testsupport.Object{
				dynamicZIPBlob: {Data: []byte("zip"), ETag: `"zip-etag"`},
			}}
			recorder := request(
				t,
				newCustomBoundaryHandler(t, storage),
				http.MethodGet,
				dynamicRoute,
				buildHeaders(t),
			)
			assertJSONError(t, recorder, http.StatusUnauthorized, "authentication_required")
			statCalls, openCalls, _ := storage.Counts()
			if len(statCalls) != 0 || len(openCalls) != 0 {
				t.Fatalf("storage calls = %#v %#v", statCalls, openCalls)
			}
		})
	}
}
