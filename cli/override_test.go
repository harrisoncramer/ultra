package cli

import (
	"context"
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLayeredSecretResolverOverrideWins(t *testing.T) {
	l := layeredSecretResolver{
		base:     mapResolver{have: map[string]string{"A": "base", "B": "base"}},
		override: mapResolver{have: map[string]string{"B": "override", "C": "override"}},
	}
	got, err := l.Resolve(context.Background(), []string{"A", "B", "C"})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"A": "base", "B": "override", "C": "override"}, got)
}

func TestLayeredSecretResolverNilOverrideIsBase(t *testing.T) {
	l := layeredSecretResolver{base: mapResolver{have: map[string]string{"A": "base"}}}
	got, err := l.Resolve(context.Background(), []string{"A"})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"A": "base"}, got)
}

func TestLayerSecretResolverNilOverridePassesBaseThrough(t *testing.T) {
	base := func(string) SecretResolver { return mapResolver{have: map[string]string{"A": "base"}} }
	got, err := layerSecretResolver(base, nil)("app").Resolve(context.Background(), []string{"A"})
	require.NoError(t, err)
	assert.Equal(t, "base", got["A"])
}

func TestLayeredConfigResolverOverrideWins(t *testing.T) {
	l := layeredConfigResolver{
		base:     configMapResolver{have: map[string]string{"X": "base", "Y": "base"}},
		override: configMapResolver{have: map[string]string{"Y": "override"}},
	}
	got, err := l.Resolve(context.Background(), "app")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"X": "base", "Y": "override"}, got)
}

func TestBuildSecretOverride(t *testing.T) {
	RegisterSecretResolver(SecretResolverCommand{
		Name: "test-override",
		Setup: func(fs *pflag.FlagSet) func(app string) SecretResolver {
			token := fs.String("token", "", "")
			return func(string) SecretResolver {
				return mapResolver{have: map[string]string{"TOKEN": *token}}
			}
		},
	})

	fc := fileConfig{override: overrideConfig{
		secretResolver: "test-override",
		secretFlags:    map[string]string{"token": "sekret"},
	}}
	factory := buildSecretOverride(fc)
	require.NotNil(t, factory)
	got, err := factory("app").Resolve(context.Background(), []string{"TOKEN"})
	require.NoError(t, err)
	assert.Equal(t, "sekret", got["TOKEN"])

	assert.Nil(t, buildSecretOverride(fileConfig{}))
}

func TestFlattenOverrideSections(t *testing.T) {
	fc := loadFrom(t, `
[secrets]
resolver = "aws-secret-manager"

[secrets.aws-secret-manager]
region = "us-east-1"

[secrets-override]
resolver = "1password"

[secrets-override.1password]
vault = "LocalDev"

[config-override]
resolver = "env"
`)
	assert.Equal(t, "1password", fc.override.secretResolver)
	assert.Equal(t, "LocalDev", fc.override.secretFlags["vault"])
	assert.Equal(t, "env", fc.override.configResolver)
	// The override's own flags must not leak into the base resolver flags.
	assert.NotContains(t, fc.flags, "vault")
}
