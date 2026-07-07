package ultra

import (
	"strings"
	"testing"
)

// ProdGroup is embedded with an envScope so its fields inherit that scope.
type ProdGroup struct {
	Prod string `env:"TS_PROD" secret:"true"`
}

type ScopedConfig struct {
	Always    string `env:"TS_ALWAYS,required,notEmpty"` // required in every environment
	ProdGroup `envScope:"production"`                    // Prod required only in production
	Local     string `env:"TS_LOCAL" envScope:"local"`   // required only in local
	Optional  string `env:"TS_OPTIONAL"`                 // never required
}

func TestLoadScopedRequired(t *testing.T) {
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
		{"sandbox needs only base", "sandbox", map[string]string{"TS_ALWAYS": "x"}, false},
		{"no environment leaves scoped optional", "", map[string]string{"TS_ALWAYS": "x"}, false},
		{"empty scoped value counts as missing", "local", map[string]string{"TS_ALWAYS": "x", "TS_LOCAL": ""}, true},
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

func TestLoadRejectsScopeWithRequired(t *testing.T) {
	type conflictConfig struct {
		Bad string `env:"TS_BAD,required" envScope:"production"`
	}
	_, err := Load(&conflictConfig{}, WithEnvironment("production"))
	if err == nil {
		t.Fatal("expected an error when a field combines envScope with required")
	}
	if !strings.Contains(err.Error(), "TS_BAD") {
		t.Fatalf("error should name the offending field, got: %v", err)
	}
}

func TestLoadWithoutEnvironmentIgnoresScope(t *testing.T) {
	// A scoped field left unset must not fail when no environment is given.
	t.Setenv("TS_ALWAYS", "x")
	if _, err := Load(&ScopedConfig{}); err != nil {
		t.Fatalf("scoped fields should be optional without an environment: %v", err)
	}
}
