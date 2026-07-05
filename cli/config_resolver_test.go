package cli

import "testing"

func TestNewConfigResolver(t *testing.T) {
	for _, kind := range []string{"docker-compose", "env"} {
		if _, err := newConfigResolver(kind, "/tmp"); err != nil {
			t.Errorf("newConfigResolver(%q): %v", kind, err)
		}
	}
	if _, err := newConfigResolver("nope", "/tmp"); err == nil {
		t.Error("expected error for unknown config resolver")
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
