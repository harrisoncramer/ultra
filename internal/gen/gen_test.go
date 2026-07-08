package gen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/harrisoncramer/ultra/internal/project"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeScanner reports a fixed set of secret names for any config dir.
type fakeScanner struct{ names []string }

func (f fakeScanner) SecretNames(string) ([]string, error) { return f.names, nil }

// fakeComposer renders a minimal, order-revealing override.
type fakeComposer struct{}

func (fakeComposer) Override(app string, names []string) string {
	return app + ":" + strings.Join(names, ",")
}

func newTestGenerator(root string, names []string) *Generator {
	return NewGenerator(NewGeneratorParams{
		Scanner:  fakeScanner{names: names},
		Composer: fakeComposer{},
		Project:  project.Project{Root: root, ConfigDir: "config"},
	})
}

func TestGenerateWritesAllDeclaredNames(t *testing.T) {
	root := t.TempDir()
	overrides, err := newTestGenerator(root, []string{"B", "A"}).Generate([]string{"app"})
	require.NoError(t, err)
	require.Len(t, overrides, 1)

	o := overrides[0]
	assert.Equal(t, "app", o.App)
	assert.Equal(t, []string{"A", "B"}, o.Names, "names are sorted for a deterministic file")

	want := filepath.Join(root, "tmp", "app.compose.yml")
	assert.Equal(t, want, o.Path)
	data, err := os.ReadFile(want)
	require.NoError(t, err)
	assert.Equal(t, "app:A,B", string(data))
}

func TestGenerateSkipsAppsWithNoSecrets(t *testing.T) {
	root := t.TempDir()
	overrides, err := newTestGenerator(root, nil).Generate([]string{"app"})
	require.NoError(t, err)
	require.Len(t, overrides, 1)

	assert.Empty(t, overrides[0].Path, "no file for an app that declares no secrets")
	_, statErr := os.Stat(filepath.Join(root, "tmp", "app.compose.yml"))
	assert.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestGeneratePreservesInputOrder(t *testing.T) {
	root := t.TempDir()
	overrides, err := newTestGenerator(root, []string{"X"}).Generate([]string{"one", "two", "three"})
	require.NoError(t, err)

	got := []string{overrides[0].App, overrides[1].App, overrides[2].App}
	assert.Equal(t, []string{"one", "two", "three"}, got)
}

func TestGenerateHonorsOverrideDir(t *testing.T) {
	root := t.TempDir()
	g := NewGenerator(NewGeneratorParams{
		Scanner:     fakeScanner{names: []string{"A"}},
		Composer:    fakeComposer{},
		Project:     project.Project{Root: root, ConfigDir: "config"},
		OverrideDir: "committed/overrides",
	})
	overrides, err := g.Generate([]string{"app"})
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, "committed", "overrides", "app.compose.yml"), overrides[0].Path)
}
