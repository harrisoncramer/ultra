package cli

import (
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type findConfigResolverCase struct {
	name      string
	resolver  string
	wantFound bool
}

func TestFindConfigResolver(t *testing.T) {
	cases := []findConfigResolverCase{
		{"docker-compose is registered", "docker-compose", true},
		{"env is registered", "env", true},
		{"unknown resolver is missed", "nope", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rc, ok := findConfigResolver(c.resolver)
			require.Equal(t, c.wantFound, ok)
			if !ok {
				return
			}
			build := rc.Setup(pflag.NewFlagSet(c.resolver, pflag.ContinueOnError))
			_, err := build("/tmp")
			assert.NoError(t, err)
		})
	}
}

type envConfigResolverCase struct {
	name string
	app  string
}

func TestEnvConfigResolverIsEmpty(t *testing.T) {
	cases := []envConfigResolverCase{
		{"env resolver provides no values", "worker"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := envConfig{}.Resolve(t.Context(), c.app)
			require.NoError(t, err)
			assert.Empty(t, got)
		})
	}
}
