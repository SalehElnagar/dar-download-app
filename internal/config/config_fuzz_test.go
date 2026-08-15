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
