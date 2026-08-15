package config_test

import (
	"testing"

	"github.com/SalehElnagar/dar-download-app/internal/config"
)

func FuzzParseEnvironmentIssuer(f *testing.F) {
	for _, issuer := range []string{
		oidcIssuer,
		"https://identity.example.com",
		"http://identity.example.com",
		"https://identity.example.com?tenant=customers",
		"https://identity.example.com/#fragment",
		string([]byte{0xff, 0x00, 0x01}),
	} {
		f.Add(issuer)
	}

	f.Fuzz(func(t *testing.T, issuer string) {
		environment := validEnvironment()
		environment[config.OIDCIssuerEnv] = issuer
		cfg, err := config.ParseEnvironment(environment)
		if err != nil {
			return
		}
		if !config.IsValidOIDCIssuer(issuer) || cfg.OIDCIssuer != issuer {
			t.Fatalf("accepted invalid or normalized issuer %q as %q", issuer, cfg.OIDCIssuer)
		}
	})
}
