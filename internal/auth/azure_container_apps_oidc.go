package auth

import (
	"net/http"

	"github.com/SalehElnagar/dar-download-app/internal/config"
	"github.com/SalehElnagar/dar-download-app/internal/strictjson"
)

func authenticateAzureContainerAppsOIDC(
	headers http.Header,
	expectedIssuer string,
	expectedProviderName string,
) (Identity, bool) {
	if !config.IsValidOIDCIssuer(expectedIssuer) ||
		!config.IsValidOIDCProviderName(expectedProviderName) {
		return Identity{}, false
	}
	evidence, ok := readCustomContainerAppsEvidence(headers)
	if !ok || evidence.identityProvider != expectedProviderName ||
		!config.IsValidOIDCSubject(evidence.principalID) ||
		!validCustomContainerAppsPrincipal(evidence.encodedPrincipal, expectedProviderName) {
		return Identity{}, false
	}
	return Identity{Issuer: expectedIssuer, Subject: evidence.principalID}, true
}

type customContainerAppsEvidence struct {
	encodedPrincipal string
	principalID      string
	identityProvider string
}

func readCustomContainerAppsEvidence(headers http.Header) (customContainerAppsEvidence, bool) {
	principalValues := headers.Values(containerAppsPrincipalHeader)
	principalIDValues := headers.Values(containerAppsPrincipalIDHeader)
	idpValues := headers.Values(containerAppsPrincipalIDPHeader)
	if len(principalValues) != 1 || len(principalIDValues) != 1 || len(idpValues) != 1 {
		return customContainerAppsEvidence{}, false
	}
	return customContainerAppsEvidence{
		encodedPrincipal: principalValues[0],
		principalID:      principalIDValues[0],
		identityProvider: idpValues[0],
	}, true
}

func validCustomContainerAppsPrincipal(encodedPrincipal, expectedProviderName string) bool {
	if encodedPrincipal == "" || len(encodedPrincipal) > maxContainerAppsPrincipalHeaderBytes {
		return false
	}
	decodedPrincipal, ok := decodeCanonicalContainerAppsPrincipal(encodedPrincipal)
	if !ok || !hasExactContainerAppsPrincipalSchema(decodedPrincipal) {
		return false
	}
	var principal containerAppsPrincipal
	if err := strictjson.Decode(decodedPrincipal, maxContainerAppsPrincipalHeaderBytes, &principal); err != nil ||
		principal.AuthenticationType != expectedProviderName ||
		len(principal.Claims) < 1 || len(principal.Claims) > maxContainerAppsPrincipalClaims {
		return false
	}
	return validContainerAppsClaims(principal.Claims)
}

func validContainerAppsClaims(claims []containerAppsClaim) bool {
	for _, claim := range claims {
		if !validContainerAppsClaimText(claim.Type, maxContainerAppsClaimTypeBytes) ||
			!validContainerAppsClaimText(claim.Value, maxContainerAppsClaimValueBytes) {
			return false
		}
	}
	return true
}
