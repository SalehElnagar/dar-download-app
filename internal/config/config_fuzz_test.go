package config_test

import (
	"testing"

	"github.com/SalehElnagar/dar-download-app/internal/config"
)

func FuzzParseEnvironmentPolicy(f *testing.F) {
	valid := validEnvironment()
	f.Add(valid[config.ReleasesJSONEnv])
	f.Add(`{}`)
	f.Add(`{"duplicate":1,"duplicate":2}`)
	f.Add(`{"dar_01JABCDEF0123456789XYZ":{"allowed_subjects":["customer:\u0001"],` +
		`"blob_name":"releases/example.dar","download_name":"example.dar"}}`)
	legacyField := "allowed_" + "principal_ids"
	f.Add(`{"dar_01JABCDEF0123456789XYZ":{"` + legacyField + `":["customer:001"],` +
		`"blob_name":"releases/example.dar","download_name":"example.dar"}}`)
	f.Add(string([]byte{0xff, 0x00, 0x01}))

	f.Fuzz(func(t *testing.T, policy string) {
		environment := validEnvironment()
		environment[config.ReleasesJSONEnv] = policy
		cfg, err := config.ParseEnvironment(environment)
		if err != nil {
			return
		}
		if cfg.ReleaseCount() < 1 || cfg.ReleaseCount() > config.MaxReleases {
			t.Fatalf("accepted invalid release count %d", cfg.ReleaseCount())
		}
	})
}
