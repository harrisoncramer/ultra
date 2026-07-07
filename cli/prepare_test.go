package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubResolver returns a fixed set of secret values, ignoring the requested names,
// so a test can simulate a store that holds only a subset of an app's secrets.
type stubResolver struct{ values map[string]string }

func (s stubResolver) Resolve(context.Context, []string) (map[string]string, error) {
	return s.values, nil
}

// writeTestApp writes a throwaway Go module with a config package declaring the
// given secret-tagged env names, and returns the repo root it lives under.
func writeTestApp(t *testing.T, names ...string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "app", "config")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "app", "go.mod"), []byte("module testapp\n\ngo 1.25\n"), 0o644))
	var b strings.Builder
	b.WriteString("package config\n\ntype Config struct {\n")
	for _, n := range names {
		b.WriteString("\t" + n + " string `env:\"" + n + "\" secret:\"true\"`\n")
	}
	b.WriteString("}\n")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.go"), []byte(b.String()), 0o644))
	return root
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
	appNames    []string          // secret env names the throwaway config declares
	resolved    map[string]string // what the stub resolver returns
	overrideDir string            // override dir under root ("" = default "tmp")
	useRun      bool              // drive through run() instead of prepare()
	command     []string          // command for run()

	wantOverride bool     // whether the override file should exist
	wantContains []string // substrings the override file must contain
	wantAbsent   []string // substrings the override file must not contain

	wantEnv          map[string]string // launcher vars that must be present (prepare only)
	wantEnvAbsentPfx []string          // launcher var prefixes that must be absent (prepare only)
}

func TestPrepare(t *testing.T) {
	cases := []prepareCase{
		{
			name:             "omits unresolved secrets from the override and env",
			appNames:         []string{"RESOLVED", "MISSING"},
			resolved:         map[string]string{"RESOLVED": "value"},
			wantOverride:     true,
			wantContains:     []string{"RESOLVED:"},
			wantAbsent:       []string{"MISSING:"},
			wantEnv:          map[string]string{"ULTRA_APP__RESOLVED": "value"},
			wantEnvAbsentPfx: []string{"ULTRA_APP__MISSING="},
		},
		{
			name:         "writes no override when nothing resolves",
			appNames:     []string{"MISSING"},
			resolved:     map[string]string{},
			wantOverride: false,
		},
		{
			name:         "run keeps the override after the command exits",
			appNames:     []string{"RESOLVED"},
			resolved:     map[string]string{"RESOLVED": "value"},
			useRun:       true,
			command:      []string{"true"},
			wantOverride: true,
		},
		{
			name:         "honors a configured override dir",
			appNames:     []string{"RESOLVED"},
			resolved:     map[string]string{"RESOLVED": "value"},
			overrideDir:  "ultra/overrides",
			wantOverride: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := writeTestApp(t, c.appNames...)
			p := runParams{
				root:        root,
				apps:        []string{"app"},
				configDir:   "config",
				overrideDir: c.overrideDir,
				command:     c.command,
				resolverFor: func(string) SecretResolver {
					return stubResolver{values: c.resolved}
				},
			}

			dir := c.overrideDir
			if dir == "" {
				dir = "tmp"
			}
			override := filepath.Join(root, dir, "app.compose.yml")

			if c.useRun {
				require.NoError(t, run(context.Background(), p))
			} else {
				prep, err := prepare(context.Background(), p)
				require.NoError(t, err)
				for k, v := range c.wantEnv {
					assert.True(t, hasEnv(prep.env, k, v), "launcher env missing %s=%s", k, v)
				}
				for _, pfx := range c.wantEnvAbsentPfx {
					for _, e := range prep.env {
						assert.False(t, strings.HasPrefix(e, pfx), "launcher env forwarded unexpected var: %q", e)
					}
				}
				assertComposeFiles(t, prep.composeFiles, override, c.wantOverride)
			}

			assertOverrideFile(t, override, c)
		})
	}
}

func TestPrepareUsesConfiguredComposeFile(t *testing.T) {
	root := writeTestApp(t, "RESOLVED")
	prep, err := prepare(context.Background(), runParams{
		root:        root,
		apps:        []string{"app"},
		configDir:   "config",
		composeFile: "docker-compose.lake.yml",
		resolverFor: func(string) SecretResolver {
			return stubResolver{values: map[string]string{"RESOLVED": "value"}}
		},
	})
	require.NoError(t, err)
	require.NotEmpty(t, prep.composeFiles)
	assert.Equal(t, filepath.Join(root, "docker-compose.lake.yml"), prep.composeFiles[0])
}

// assertComposeFiles checks the generated override is listed in composeFiles iff
// it was expected to be written.
func assertComposeFiles(t *testing.T, composeFiles []string, override string, want bool) {
	t.Helper()
	if want {
		assert.Contains(t, composeFiles, override)
	} else {
		assert.NotContains(t, composeFiles, override)
	}
}

// assertOverrideFile checks the override file's existence and contents against
// the case's expectations.
func assertOverrideFile(t *testing.T, override string, c prepareCase) {
	t.Helper()
	data, err := os.ReadFile(override)
	if !c.wantOverride {
		assert.ErrorIs(t, err, os.ErrNotExist)
		return
	}
	require.NoError(t, err)
	got := string(data)
	for _, s := range c.wantContains {
		assert.Contains(t, got, s)
	}
	for _, s := range c.wantAbsent {
		assert.NotContains(t, got, s)
	}
}
