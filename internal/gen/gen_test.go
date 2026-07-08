package gen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/harrisoncramer/ultra/internal/project"
	pkgcompose "github.com/harrisoncramer/ultra/pkg/compose"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeScanner reports a fixed set of secret names for any config dir.
type fakeScanner struct{ names []string }

func (f fakeScanner) SecretNames(string) ([]string, error) { return f.names, nil }

// fakeComposer renders a minimal, order-revealing combined override: one
// "app=names" line per app block, in the order it was given them.
type fakeComposer struct{}

func (fakeComposer) Override(apps []pkgcompose.AppSecrets) string {
	var b strings.Builder
	for _, a := range apps {
		b.WriteString(a.App + "=" + strings.Join(a.Names, ",") + "\n")
	}
	return b.String()
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
	result, err := newTestGenerator(root, []string{"B", "A"}).Generate([]string{"app"})
	require.NoError(t, err)
	require.Len(t, result.Apps, 1)

	o := result.Apps[0]
	assert.Equal(t, "app", o.App)
	assert.Equal(t, []string{"A", "B"}, o.Names, "names are sorted for a deterministic file")

	want := filepath.Join(root, "tmp", "ultra.compose.yml")
	assert.Equal(t, want, result.Path)
	data, err := os.ReadFile(want)
	require.NoError(t, err)
	assert.Equal(t, "app=A,B\n", string(data))
}

func TestGenerateCombinesAppsIntoOneFile(t *testing.T) {
	root := t.TempDir()
	result, err := newTestGenerator(root, []string{"X"}).Generate([]string{"one", "two"})
	require.NoError(t, err)

	want := filepath.Join(root, "tmp", "ultra.compose.yml")
	assert.Equal(t, want, result.Path, "both apps land in a single file")
	data, err := os.ReadFile(want)
	require.NoError(t, err)
	assert.Equal(t, "one=X\ntwo=X\n", string(data), "each app contributes a block in input order")
}

func TestGenerateSkipsAppsWithNoSecrets(t *testing.T) {
	root := t.TempDir()
	result, err := newTestGenerator(root, nil).Generate([]string{"app"})
	require.NoError(t, err)
	require.Len(t, result.Apps, 1)

	assert.Empty(t, result.Apps[0].Names, "an app that declares no secrets contributes no names")
	assert.Empty(t, result.Path, "no file when no app declares a secret")
	_, statErr := os.Stat(filepath.Join(root, "tmp", "ultra.compose.yml"))
	assert.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestGeneratePreservesInputOrder(t *testing.T) {
	root := t.TempDir()
	result, err := newTestGenerator(root, []string{"X"}).Generate([]string{"one", "two", "three"})
	require.NoError(t, err)

	got := []string{result.Apps[0].App, result.Apps[1].App, result.Apps[2].App}
	assert.Equal(t, []string{"one", "two", "three"}, got)
}

func TestGenerateHonorsOutputDir(t *testing.T) {
	root := t.TempDir()
	g := NewGenerator(NewGeneratorParams{
		Scanner:   fakeScanner{names: []string{"A"}},
		Composer:  fakeComposer{},
		Project:   project.Project{Root: root, ConfigDir: "config"},
		OutputDir: "committed/overrides",
	})
	result, err := g.Generate([]string{"app"})
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, "committed", "overrides", "ultra.compose.yml"), result.Path)
}

func TestGenerateHonorsOutputFilename(t *testing.T) {
	root := t.TempDir()
	g := NewGenerator(NewGeneratorParams{
		Scanner:        fakeScanner{names: []string{"A"}},
		Composer:       fakeComposer{},
		Project:        project.Project{Root: root, ConfigDir: "config"},
		OutputFilename: "docker-compose.override.yml",
	})
	result, err := g.Generate([]string{"app"})
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, "tmp", "docker-compose.override.yml"), result.Path)
}
