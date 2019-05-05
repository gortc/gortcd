package cli

import (
	"testing"

	"github.com/gortc/gortcd/internal/server"
)

func TestConfig(t *testing.T) {
	t.Run("Default", func(t *testing.T) {
		initConfig()
		logCfg, logErr := getZapConfig()
		if logErr != nil {
			t.Fatal(logErr)
		}
		l, buildErr := logCfg.Build()
		if buildErr != nil {
			t.Fatal(buildErr)
		}
		opt := server.Options{}
		if err := parseOptions(l, &opt); err != nil {
			t.Fatal(err)
		}
	})
}
