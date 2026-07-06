package ultra

import (
	"reflect"
	"sort"
	"testing"

	secrets "github.com/harrisoncramer/ultra/pkg/secrets"
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
	got := secrets.SecretEnvNames(reflect.TypeFor[composedConfig]())
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

func TestLoadFailsOnMissingRequiredSecret(t *testing.T) {
	type cfg struct {
		Token string `env:"REQUIRED_SECRET_TOKEN,required,notEmpty" secret:"true"`
	}
	if _, err := Load[cfg](); err == nil {
		t.Fatal("expected an error when a required secret is unset")
	}
}

func TestLoadAllowsMissingOptionalSecret(t *testing.T) {
	type cfg struct {
		Token string `env:"OPTIONAL_SECRET_TOKEN" secret:"true"`
	}
	if _, err := Load[cfg](); err != nil {
		t.Fatalf("optional secret unset should not fail: %v", err)
	}
}
