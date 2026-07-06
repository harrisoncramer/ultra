package cli

import (
	"testing"

	"github.com/spf13/pflag"
)

func TestConfigResolverSetup(t *testing.T) {
	for _, kind := range []string{"docker-compose", "env"} {
		rc, ok := findConfigResolver(kind)
		if !ok {
			t.Fatalf("findConfigResolver(%q): not registered", kind)
		}
		build := rc.Setup(pflag.NewFlagSet(kind, pflag.ContinueOnError))
		if _, err := build("/tmp"); err != nil {
			t.Errorf("building %q: %v", kind, err)
		}
	}
	if _, ok := findConfigResolver("nope"); ok {
		t.Error("expected findConfigResolver to miss for unknown config resolver")
	}
}

func TestEnvConfigResolverIsEmpty(t *testing.T) {
	got, err := envConfig{}.Resolve(t.Context(), "worker")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("env config resolver returned %v, want empty", got)
	}
}
