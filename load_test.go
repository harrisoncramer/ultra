package ultra

import (
	"bytes"
	"log/slog"
	"reflect"
	"sort"
	"strings"
	"testing"
)

type nestedSecrets struct {
	B string `env:"B_TOKEN" secret:"true"`
}

type baseSecrets struct {
	A string `env:"A_TOKEN" secret:"true"`
}

type composedConfig struct {
	baseSecrets
	Extra  nestedSecrets
	Ptr    *nestedSecrets
	Direct string `env:"C_TOKEN" secret:"true"`
	Plain  string `env:"PLAIN"`
}

func TestSecretEnvNamesRecurses(t *testing.T) {
	got := secretEnvNames(reflect.TypeFor[composedConfig]())
	sort.Strings(got)
	want := []string{"A_TOKEN", "B_TOKEN", "C_TOKEN"} // B_TOKEN deduped across Extra + Ptr
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestLoadWarnsOnMissingSecret(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	defer slog.SetDefault(prev)

	type cfg struct {
		Token string `env:"MISSING_SECRET_TOKEN" secret:"true"`
	}
	// Not required, so parse succeeds; the point is the warning.
	if _, err := Load[cfg](); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.Contains(buf.String(), "MISSING_SECRET_TOKEN") {
		t.Fatalf("expected a warning naming MISSING_SECRET_TOKEN, got: %q", buf.String())
	}
}
