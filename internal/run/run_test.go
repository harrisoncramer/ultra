package run

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/harrisoncramer/ultra/internal/project"
	"github.com/harrisoncramer/ultra/internal/resolve"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeScanner reports a fixed set of secret names for any config dir.
type fakeScanner struct{ names []string }

func (f fakeScanner) SecretNames(string) ([]string, error) { return f.names, nil }

// fakeComposer renders predictable launcher variables and a minimal override.
type fakeComposer struct{}

func (fakeComposer) Var(app, name string) string {
	return "ULTRA_" + strings.ToUpper(app) + "__" + name
}

func (fakeComposer) Override(_ string, names []string) string {
	var b strings.Builder
	for _, n := range names {
		b.WriteString(n + ":\n")
	}
	return b.String()
}

// stubResolver returns a fixed set of secret values, simulating a store that
// holds only a subset of an app's secrets.
type stubResolver struct{ values map[string]string }

func (s stubResolver) Resolve(context.Context, []string) (map[string]string, error) {
	return s.values, nil
}

func resolverFor(values map[string]string) func(string) resolve.SecretResolver {
	return func(string) resolve.SecretResolver { return stubResolver{values: values} }
}

func hasEnv(env []string, key, val string) bool {
	want := key + "=" + val
	for _, e := range env {
		if e == want {
			return true
		}
	}
	return false
}

type prepareCase struct {
	name        string
	names       []string          // secret names the fake scanner reports
	resolved    map[string]string // what the stub resolver returns
	overrideDir string            // "" = default "tmp"
	composeFile string            // "" = default "docker-compose.yml"

	wantOverride     bool
	wantContains     []string
	wantAbsent       []string
	wantEnv          map[string]string
	wantEnvAbsentPfx []string
}

func TestPrepare(t *testing.T) {
	cases := []prepareCase{
		{
			name:             "omits unresolved secrets from the override and env",
			names:            []string{"RESOLVED", "MISSING"},
			resolved:         map[string]string{"RESOLVED": "value"},
			wantOverride:     true,
			wantContains:     []string{"RESOLVED:"},
			wantAbsent:       []string{"MISSING:"},
			wantEnv:          map[string]string{"ULTRA_APP__RESOLVED": "value"},
			wantEnvAbsentPfx: []string{"ULTRA_APP__MISSING="},
		},
		{
			name:         "writes no override when nothing resolves",
			names:        []string{"MISSING"},
			resolved:     map[string]string{},
			wantOverride: false,
		},
		{
			name:         "honors a configured override dir",
			names:        []string{"RESOLVED"},
			resolved:     map[string]string{"RESOLVED": "value"},
			overrideDir:  "ultra/overrides",
			wantOverride: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			runner := NewRunner(NewRunnerParams{
				Scanner:     fakeScanner{names: c.names},
				Composer:    fakeComposer{},
				Project:     project.Project{Root: root, ConfigDir: "config"},
				OverrideDir: c.overrideDir,
				ComposeFile: c.composeFile,
			})

			prep, err := runner.Prepare(context.Background(), Params{
				Apps:        []string{"app"},
				ResolverFor: resolverFor(c.resolved),
			})
			require.NoError(t, err)

			for k, v := range c.wantEnv {
				assert.True(t, hasEnv(prep.Env, k, v), "env missing %s=%s", k, v)
			}
			for _, pfx := range c.wantEnvAbsentPfx {
				for _, e := range prep.Env {
					assert.False(t, strings.HasPrefix(e, pfx), "env forwarded unexpected var: %q", e)
				}
			}

			dir := c.overrideDir
			if dir == "" {
				dir = "tmp"
			}
			override := filepath.Join(root, dir, "app.compose.yml")
			data, err := os.ReadFile(override)
			if !c.wantOverride {
				assert.ErrorIs(t, err, os.ErrNotExist)
				assert.NotContains(t, prep.ComposeFiles, override)
				return
			}
			require.NoError(t, err)
			assert.Contains(t, prep.ComposeFiles, override)
			for _, s := range c.wantContains {
				assert.Contains(t, string(data), s)
			}
			for _, s := range c.wantAbsent {
				assert.NotContains(t, string(data), s)
			}
		})
	}
}

func TestPrepareUsesConfiguredComposeFile(t *testing.T) {
	root := t.TempDir()
	runner := NewRunner(NewRunnerParams{
		Scanner:     fakeScanner{names: []string{"RESOLVED"}},
		Composer:    fakeComposer{},
		Project:     project.Project{Root: root, ConfigDir: "config"},
		ComposeFile: "docker-compose.lake.yml",
	})
	prep, err := runner.Prepare(context.Background(), Params{
		Apps:        []string{"app"},
		ResolverFor: resolverFor(map[string]string{"RESOLVED": "value"}),
	})
	require.NoError(t, err)
	require.NotEmpty(t, prep.ComposeFiles)
	assert.Equal(t, filepath.Join(root, "docker-compose.lake.yml"), prep.ComposeFiles[0])
}

func TestRunKeepsOverrideAfterExit(t *testing.T) {
	root := t.TempDir()
	runner := NewRunner(NewRunnerParams{
		Scanner:  fakeScanner{names: []string{"RESOLVED"}},
		Composer: fakeComposer{},
		Project:  project.Project{Root: root, ConfigDir: "config"},
	})
	err := runner.Run(context.Background(), Params{
		Apps:        []string{"app"},
		ResolverFor: resolverFor(map[string]string{"RESOLVED": "value"}),
		Command:     []string{"true"},
	})
	require.NoError(t, err)
	_, statErr := os.Stat(filepath.Join(root, "tmp", "app.compose.yml"))
	assert.NoError(t, statErr, "override should be kept after run")
}
