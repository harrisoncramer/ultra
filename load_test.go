package ultra

import (
	"reflect"
	"sort"
	"testing"

	secrets "github.com/harrisoncramer/ultra/pkg/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

type secretEnvNamesCase struct {
	name string
	typ  reflect.Type
	want []string
}

func TestSecretEnvNames(t *testing.T) {
	cases := []secretEnvNamesCase{
		{
			name: "recurses embedded, nested and pointer structs, dedups shared names",
			typ:  reflect.TypeFor[composedConfig](),
			want: []string{"A_TOKEN", "B_TOKEN", "C_TOKEN"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := secrets.SecretEnvNames(c.typ)
			sort.Strings(got)
			assert.Equal(t, c.want, got)
		})
	}
}

type loadSecretCase struct {
	name    string
	load    func() error
	wantErr bool
}

func TestLoadSecretRequiredness(t *testing.T) {
	cases := []loadSecretCase{
		{
			name: "env-tag required secret is rejected",
			load: func() error {
				type cfg struct {
					Token string `env:"REQUIRED_SECRET_TOKEN,required,notEmpty" secret:"true"`
				}
				_, err := Load(&cfg{})
				return err
			},
			wantErr: true,
		},
		{
			name: "optional secret left unset does not fail",
			load: func() error {
				type cfg struct {
					Token string `env:"OPTIONAL_SECRET_TOKEN" secret:"true"`
				}
				_, err := Load(&cfg{})
				return err
			},
			wantErr: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.load()
			if c.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
