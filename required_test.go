package ultra

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ProdGroup is embedded with a required tag, so its fields inherit those
// environments unless they declare their own.
type ProdGroup struct {
	Prod string `env:"TS_PROD" secret:"true"`
}

type ScopedConfig struct {
	Always    string                  `env:"TS_ALWAYS" required:"*"` // required in every environment
	ProdGroup `required:"production"` // Prod required only in production
	Local     string                  `env:"TS_LOCAL" required:"local"`
	Optional  string                  `env:"TS_OPTIONAL"` // never required
}

type requiredEnvCase struct {
	name    string
	env     string
	set     map[string]string
	wantErr bool
}

func TestLoadRequiredByEnvironment(t *testing.T) {
	cases := []requiredEnvCase{
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
			if c.wantErr {
				require.Error(t, err, "env %q with %v", c.env, c.set)
			} else {
				require.NoError(t, err, "env %q with %v", c.env, c.set)
			}
		})
	}
}

type rejectEnvTagCase struct {
	name         string
	load         func() error
	wantContains string
}

func TestLoadRejectsEnvTagOptions(t *testing.T) {
	cases := []rejectEnvTagCase{
		{
			name: "required declared in the env tag",
			load: func() error {
				type badConfig struct {
					Bad string `env:"TS_BAD,required"`
				}
				_, err := Load(&badConfig{})
				return err
			},
			wantContains: "TS_BAD",
		},
		{
			name: "notEmpty declared in the env tag",
			load: func() error {
				type badConfig struct {
					Bad string `env:"TS_BAD_EMPTY,notEmpty"`
				}
				_, err := Load(&badConfig{})
				return err
			},
			wantContains: "TS_BAD_EMPTY",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.load()
			require.Error(t, err)
			assert.ErrorContains(t, err, c.wantContains)
		})
	}
}
