package resolve

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mapResolver is a secret or config resolver returning a fixed set of keys.
type mapResolver struct{ have map[string]string }

func (m mapResolver) Resolve(context.Context, []string) (map[string]string, error) {
	return m.have, nil
}

type configMapResolver struct{ have map[string]string }

func (c configMapResolver) Resolve(context.Context, string) (map[string]string, error) {
	return c.have, nil
}

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
	got, err := LayerSecretResolver(base, nil)("app").Resolve(context.Background(), []string{"A"})
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

func TestLiteralLeaks(t *testing.T) {
	env := map[string]string{
		"HARDCODED":  "sk_live_abc123",
		"FORWARDED":  "${DATABASE_URL}",
		"PARTIAL":    "prefix-${TOKEN}",
		"EMPTY":      "",
		"NON_SECRET": "info",
	}
	got := literalLeaks(env, []string{"HARDCODED", "FORWARDED", "PARTIAL", "EMPTY", "ABSENT"})
	assert.Equal(t, []string{"HARDCODED"}, got)
}

// leakyConfigResolver is a config resolver that also reports a fixed set of
// hardcoded secret names, exercising the SecretLeakChecker path.
type leakyConfigResolver struct {
	configMapResolver
	hardcoded []string
}

func (l leakyConfigResolver) LeakedSecrets(_ context.Context, _ string, names []string) ([]string, error) {
	want := make(map[string]bool, len(l.hardcoded))
	for _, n := range l.hardcoded {
		want[n] = true
	}
	var out []string
	for _, n := range names {
		if want[n] {
			out = append(out, n)
		}
	}
	return out, nil
}

func TestLayeredConfigResolverLeakedSecretsUnion(t *testing.T) {
	l := layeredConfigResolver{
		base:     leakyConfigResolver{hardcoded: []string{"A", "B"}},
		override: leakyConfigResolver{hardcoded: []string{"B", "C"}},
	}
	got, err := l.LeakedSecrets(context.Background(), "app", []string{"A", "B", "C", "D"})
	require.NoError(t, err)
	assert.Equal(t, []string{"A", "B", "C"}, got)
}

func TestLayeredConfigResolverLeakedSecretsSkipsNonCheckers(t *testing.T) {
	l := layeredConfigResolver{
		base:     configMapResolver{have: map[string]string{"A": "x"}},
		override: leakyConfigResolver{hardcoded: []string{"A"}},
	}
	got, err := l.LeakedSecrets(context.Background(), "app", []string{"A"})
	require.NoError(t, err)
	assert.Equal(t, []string{"A"}, got)
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

	factory := BuildSecretOverride("test-override", map[string]string{"token": "sekret"})
	require.NotNil(t, factory)
	got, err := factory("app").Resolve(context.Background(), []string{"TOKEN"})
	require.NoError(t, err)
	assert.Equal(t, "sekret", got["TOKEN"])

	assert.Nil(t, BuildSecretOverride("", nil))
	assert.Nil(t, BuildSecretOverride("unknown", nil))
}

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
			rc, ok := FindConfigResolver(c.resolver)
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

func TestEnvConfigResolverIsEmpty(t *testing.T) {
	got, err := envConfig{}.Resolve(context.Background(), "worker")
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestDockerComposeResolverHonorsComposeFile(t *testing.T) {
	rc, ok := FindConfigResolver("docker-compose")
	require.True(t, ok)
	fs := pflag.NewFlagSet("docker-compose", pflag.ContinueOnError)
	build := rc.Setup(fs)
	require.NoError(t, fs.Set("compose-file", "docker-compose.lake.yml"))

	cr, err := build("/repo")
	require.NoError(t, err)
	dc, ok := cr.(*dockerComposeConfig)
	require.True(t, ok)
	assert.Equal(t, filepath.Join("/repo", "docker-compose.lake.yml"), dc.composeFile)
}
