package run

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/harrisoncramer/ultra/internal/configreader"
	"github.com/harrisoncramer/ultra/internal/project"
	"github.com/harrisoncramer/ultra/internal/resolve"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestRunner builds a Runner backed by a ConfigReader over the fake scanner,
// so tests exercise the real config-reading path. overridePath is where the
// committed override is expected to live; run points COMPOSE_FILE at it but
// never writes it.
func newTestRunner(root, composeFile, overridePath string, names []string) *Runner {
	proj := project.Project{Root: root, ConfigDir: "config"}
	return NewRunner(NewRunnerParams{
		Reader:       configreader.NewConfigReader(configreader.NewConfigReaderParams{Scanner: fakeScanner{names: names}, Project: proj}),
		Composer:     fakeComposer{},
		Project:      proj,
		ComposeFile:  composeFile,
		OverridePath: overridePath,
	})
}

// fakeScanner reports a fixed set of secret names for any config dir.
type fakeScanner struct{ names []string }

func (f fakeScanner) SecretNames(string) ([]string, error) { return f.names, nil }

// fakeComposer renders predictable launcher variables.
type fakeComposer struct{}

func (fakeComposer) Var(app, name string) string {
	return "ULTRA_" + strings.ToUpper(app) + "__" + name
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

// writeOverride simulates gen having already written the committed override at
// path, so run has a file to point COMPOSE_FILE at.
func writeOverride(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("services: {}\n"), 0o644))
}

// TestPrepareResolvesAndPointsAtOverride: with the committed override present,
// prepare resolves declared secrets into the env and appends the override to the
// compose files, without writing anything.
func TestPrepareResolvesAndPointsAtOverride(t *testing.T) {
	root := t.TempDir()
	override := filepath.Join(root, "tmp", "ultra.compose.yml")
	writeOverride(t, override)
	before, err := os.ReadFile(override)
	require.NoError(t, err)

	runner := newTestRunner(root, "", override, []string{"RESOLVED", "MISSING"})
	prep, err := runner.prepare(context.Background(), Params{
		Apps:        []string{"app"},
		ResolverFor: resolverFor(map[string]string{"RESOLVED": "value"}),
	})
	require.NoError(t, err)

	assert.True(t, hasEnv(prep.env, "ULTRA_APP__RESOLVED", "value"), "resolved secret forwarded")
	for _, e := range prep.env {
		assert.False(t, strings.HasPrefix(e, "ULTRA_APP__MISSING="), "unresolved secret must not be forwarded: %q", e)
	}
	assert.Contains(t, prep.composeFiles, override, "override appended to compose files")

	after, err := os.ReadFile(override)
	require.NoError(t, err)
	assert.Equal(t, before, after, "run must not write or modify the override")
}

// TestPrepareErrorsWhenOverrideMissing: declaring secrets but with no committed
// override is a hard error telling the user to run gen; run does not create it.
func TestPrepareErrorsWhenOverrideMissing(t *testing.T) {
	root := t.TempDir()
	override := filepath.Join(root, "tmp", "ultra.compose.yml")

	runner := newTestRunner(root, "", override, []string{"RESOLVED"})
	_, err := runner.prepare(context.Background(), Params{
		Apps:        []string{"app"},
		ResolverFor: resolverFor(map[string]string{"RESOLVED": "value"}),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ultra gen")

	_, statErr := os.Stat(override)
	assert.ErrorIs(t, statErr, os.ErrNotExist, "run must not write the override")
}

// TestPrepareNoSecretsSkipsOverride: an app that declares no secrets needs no
// override, so prepare succeeds with only the base compose and requires no file.
func TestPrepareNoSecretsSkipsOverride(t *testing.T) {
	root := t.TempDir()
	override := filepath.Join(root, "tmp", "ultra.compose.yml")

	runner := newTestRunner(root, "", override, nil)
	prep, err := runner.prepare(context.Background(), Params{
		Apps:        []string{"app"},
		ResolverFor: resolverFor(nil),
	})
	require.NoError(t, err)
	assert.NotContains(t, prep.composeFiles, override)

	_, statErr := os.Stat(override)
	assert.ErrorIs(t, statErr, os.ErrNotExist)
}

// TestPrepareUsesConfiguredComposeFile: the base compose file is the first
// entry in the compose file list and honors the configured name.
func TestPrepareUsesConfiguredComposeFile(t *testing.T) {
	root := t.TempDir()
	override := filepath.Join(root, "tmp", "ultra.compose.yml")
	writeOverride(t, override)

	runner := newTestRunner(root, "docker-compose.lake.yml", override, []string{"RESOLVED"})
	prep, err := runner.prepare(context.Background(), Params{
		Apps:        []string{"app"},
		ResolverFor: resolverFor(map[string]string{"RESOLVED": "value"}),
	})
	require.NoError(t, err)
	require.NotEmpty(t, prep.composeFiles)
	assert.Equal(t, filepath.Join(root, "docker-compose.lake.yml"), prep.composeFiles[0])
}

// TestRunWritesNothing: a full Run leaves the pre-existing override untouched.
func TestRunWritesNothing(t *testing.T) {
	root := t.TempDir()
	override := filepath.Join(root, "tmp", "ultra.compose.yml")
	writeOverride(t, override)
	before, err := os.ReadFile(override)
	require.NoError(t, err)

	runner := newTestRunner(root, "", override, []string{"RESOLVED"})
	err = runner.Run(context.Background(), Params{
		Apps:        []string{"app"},
		ResolverFor: resolverFor(map[string]string{"RESOLVED": "value"}),
		Command:     []string{"true"},
	})
	require.NoError(t, err)

	after, err := os.ReadFile(override)
	require.NoError(t, err)
	assert.Equal(t, before, after, "run must not rewrite the override")
}
