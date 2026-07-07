package ultra

import (
	"strings"
	"testing"
)

// ProdGroup is embedded with a required tag, so its fields inherit those
// environments unless they declare their own.
type ProdGroup struct {
	Prod string `env:"TS_PROD" secret:"true"`
}

type ScopedConfig struct {
	Always    string `env:"TS_ALWAYS" required:"*"` // required in every environment
	ProdGroup `required:"production"`                // Prod required only in production
	Local     string `env:"TS_LOCAL" required:"local"`
	Optional  string `env:"TS_OPTIONAL"` // never required
}

func TestLoadRequiredByEnvironment(t *testing.T) {
	cases := []struct {
		name    string
		env     string
		set     map[string]string
		wantErr bool
	}{
		{"local with local var", "local", map[string]string{"TS_ALWAYS": "x", "TS_LOCAL": "y"}, false},
		{"local missing local var", "local", map[string]string{"TS_ALWAYS": "x"}, true},
		{"production missing prod var", "production", map[string]string{"TS_ALWAYS": "x"}, true},
		{"production with prod var, local not needed", "production", map[string]string{"TS_ALWAYS": "x", "TS_PROD": "z"}, false},
		{"sandbox needs only the always field", "sandbox", map[string]string{"TS_ALWAYS": "x"}, false},
		{"missing the always field fails everywhere", "sandbox", map[string]string{}, true},
		{"no environment still enforces required:*", "", map[string]string{}, true},
		{"no environment leaves listed envs optional", "", map[string]string{"TS_ALWAYS": "x"}, false},
		{"empty value counts as missing", "local", map[string]string{"TS_ALWAYS": "x", "TS_LOCAL": ""}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for k, v := range c.set {
				t.Setenv(k, v)
			}
			_, err := Load(&ScopedConfig{}, WithEnvironment(c.env))
			if c.wantErr && err == nil {
				t.Fatalf("env %q with %v: expected error, got nil", c.env, c.set)
			}
			if !c.wantErr && err != nil {
				t.Fatalf("env %q with %v: unexpected error: %v", c.env, c.set, err)
			}
		})
	}
}

func TestLoadRejectsEnvTagRequired(t *testing.T) {
	type badConfig struct {
		Bad string `env:"TS_BAD,required"`
	}
	_, err := Load(&badConfig{})
	if err == nil {
		t.Fatal("expected an error when required is declared in the env tag")
	}
	if !strings.Contains(err.Error(), "TS_BAD") {
		t.Fatalf("error should name the offending field, got: %v", err)
	}
}

func TestLoadRejectsEnvTagNotEmpty(t *testing.T) {
	type badConfig struct {
		Bad string `env:"TS_BAD_EMPTY,notEmpty"`
	}
	if _, err := Load(&badConfig{}); err == nil {
		t.Fatal("expected an error when notEmpty is declared in the env tag")
	}
}
