package resolve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestComposeScalarUnmarshal covers the coercion of docker-compose environment
// values: `docker compose config --format json` emits string, number, bool, or
// null, and all non-null scalars must become their string form.
func TestComposeScalarUnmarshal(t *testing.T) {
	cases := []struct {
		name string
		json string
		want string
		set  bool
	}{
		{"string", `"info"`, "info", true},
		{"integer", `1`, "1", true},
		{"float", `0.5`, "0.5", true},
		{"bool true", `true`, "true", true},
		{"bool false", `false`, "false", true},
		{"null is unset", `null`, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var s composeScalar
			require.NoError(t, json.Unmarshal([]byte(c.json), &s))
			assert.Equal(t, c.set, s.set)
			assert.Equal(t, c.want, s.value)
		})
	}
}

// TestComposeEnvUnmarshal covers the two shapes `docker compose config --format
// json` emits for a service's environment across compose versions: an object
// keyed by name and a list of "KEY=VALUE" entries. Both must decode to the same
// map, and null must yield an empty block.
func TestComposeEnvUnmarshal(t *testing.T) {
	cases := []struct {
		name string
		json string
		want map[string]string
	}{
		{
			name: "object shape",
			json: `{"FOO":"bar","NUM":1,"FLAG":false,"UNSET":null}`,
			want: map[string]string{"FOO": "bar", "NUM": "1", "FLAG": "false"},
		},
		{
			name: "list shape",
			json: `["FOO=bar","NUM=1","FLAG=false"]`,
			want: map[string]string{"FOO": "bar", "NUM": "1", "FLAG": "false"},
		},
		{
			name: "list value with equals sign",
			json: `["DSN=postgres://u:p@h/db?sslmode=require"]`,
			want: map[string]string{"DSN": "postgres://u:p@h/db?sslmode=require"},
		},
		{
			name: "list reference is preserved",
			json: `["FORWARD=${DATABASE_URL}"]`,
			want: map[string]string{"FORWARD": "${DATABASE_URL}"},
		},
		{
			name: "list entry without equals is unset",
			json: `["PASSTHROUGH","FOO=bar"]`,
			want: map[string]string{"FOO": "bar"},
		},
		{
			name: "null is empty",
			json: `null`,
			want: map[string]string{},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var e composeEnv
			require.NoError(t, json.Unmarshal([]byte(c.json), &e))
			got := make(map[string]string, len(e))
			for k, v := range e {
				if v.set {
					got[k] = v.value
				}
			}
			assert.Equal(t, c.want, got)
		})
	}
}

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

func TestLayeredSecretResolverEmptyOverrideFallsThroughToBase(t *testing.T) {
	l := layeredSecretResolver{
		base:     mapResolver{have: map[string]string{"A": "base", "B": "base"}},
		override: mapResolver{have: map[string]string{}},
	}
	got, err := l.Resolve(context.Background(), []string{"A", "B"})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"A": "base", "B": "base"}, got)
}

// errResolver is a secret resolver that always fails with a fixed error.
type errResolver struct{ err error }

func (e errResolver) Resolve(context.Context, []string) (map[string]string, error) {
	return nil, e.err
}

func TestLayeredSecretResolverOverrideNotFoundFallsThroughToBase(t *testing.T) {
	l := layeredSecretResolver{
		base:     mapResolver{have: map[string]string{"A": "base"}},
		override: errResolver{err: fmt.Errorf("op item get: %w", ErrSecretNotFound)},
	}
	got, err := l.Resolve(context.Background(), []string{"A"})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"A": "base"}, got)
}

func TestLayeredSecretResolverOverrideOtherErrorIsFatal(t *testing.T) {
	l := layeredSecretResolver{
		base:     mapResolver{have: map[string]string{"A": "base"}},
		override: errResolver{err: errors.New("1password auth failed")},
	}
	_, err := l.Resolve(context.Background(), []string{"A"})
	require.Error(t, err)
}

func TestLayeredSecretResolverBaseNotFoundIsFatal(t *testing.T) {
	l := layeredSecretResolver{
		base:     errResolver{err: fmt.Errorf("op item get: %w", ErrSecretNotFound)},
		override: mapResolver{have: map[string]string{"A": "override"}},
	}
	_, err := l.Resolve(context.Background(), []string{"A"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSecretNotFound)
}

// blockingResolver signals when its Resolve is entered and blocks until released,
// so a test can prove two resolvers run at the same time.
type blockingResolver struct {
	entered chan struct{}
	release chan struct{}
	have    map[string]string
}

func (b blockingResolver) Resolve(context.Context, []string) (map[string]string, error) {
	close(b.entered)
	<-b.release
	return b.have, nil
}

// The base and override stores are queried concurrently: both are inside Resolve
// at the same time before either returns, rather than one waiting on the other.
func TestLayeredSecretResolverQueriesConcurrently(t *testing.T) {
	base := blockingResolver{entered: make(chan struct{}), release: make(chan struct{}), have: map[string]string{"A": "base"}}
	override := blockingResolver{entered: make(chan struct{}), release: make(chan struct{}), have: map[string]string{"B": "override"}}
	l := layeredSecretResolver{base: base, override: override}

	done := make(chan map[string]string, 1)
	go func() {
		got, err := l.Resolve(context.Background(), []string{"A", "B"})
		require.NoError(t, err)
		done <- got
	}()

	// Neither goroutine can finish until released, so both entering proves overlap.
	select {
	case <-base.entered:
	case <-time.After(time.Second):
		t.Fatal("base resolver never started")
	}
	select {
	case <-override.entered:
	case <-time.After(time.Second):
		t.Fatal("override resolver did not start while base was blocked")
	}
	close(base.release)
	close(override.release)

	assert.Equal(t, map[string]string{"A": "base", "B": "override"}, <-done)
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
		"HARDCODED":      "sk_live_abc123",
		"FORWARDED":      "${DATABASE_URL}",
		"PARTIAL":        "prefix-${TOKEN}",
		"BARE_FORWARD":   "$DATABASE_URL",
		"ESCAPED_DOLLAR": "pa$$word",
		"ESCAPED_BRACE":  "pre$${SUFFIX}",
		"EMPTY":          "",
		"NON_SECRET":     "info",
	}
	got := literalLeaks(env, []string{"HARDCODED", "FORWARDED", "PARTIAL", "BARE_FORWARD", "ESCAPED_DOLLAR", "ESCAPED_BRACE", "EMPTY", "ABSENT"})
	// A bare-dollar forward is a reference, not a leak; an escaped $$ is a literal
	// dollar, so those values are hardcoded secrets that must be flagged.
	assert.Equal(t, []string{"HARDCODED", "ESCAPED_DOLLAR", "ESCAPED_BRACE"}, got)
}

func TestContainerValue(t *testing.T) {
	// Interpolated read: $$ collapses to the single $ the container receives.
	assert.Equal(t, "pa$word", containerValue("pa$$word", false))
	assert.Equal(t, "a$b$c", containerValue("a$$b$$c", false))
	assert.Equal(t, "plain", containerValue("plain", false))
	// No-interpolate read: left raw so leak detection sees the escape and any
	// ${VAR} forward exactly as written.
	assert.Equal(t, "pa$$word", containerValue("pa$$word", true))
	assert.Equal(t, "${VAR}", containerValue("${VAR}", true))
}

func TestHasVarReference(t *testing.T) {
	cases := []struct {
		v    string
		want bool
	}{
		{"${VAR}", true},
		{"$VAR", true},
		{"prefix-${TOKEN}-suffix", true},
		{"a$VAR", true},
		{"${VAR:-default}", true},
		{"sk_live_abc123", false},
		{"pa$$word", false},
		{"pre$${SUFFIX}", false},
		{"trailing$", false},
		{"$1000", false},
		{"", false},
	}
	for _, c := range cases {
		t.Run(c.v, func(t *testing.T) {
			assert.Equal(t, c.want, hasVarReference(c.v))
		})
	}
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
