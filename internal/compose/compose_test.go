package compose

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServiceNames(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "docker-compose.yml")
	body := `services:
  auth:
    build: .
  axle:
    image: axle
volumes:
  data:
`
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))

	names, err := ServiceNames(path)
	require.NoError(t, err)
	assert.Equal(t, map[string]bool{"auth": true, "axle": true}, names, "only top-level service keys, not volumes")
}

func TestServiceNamesMissingFile(t *testing.T) {
	_, err := ServiceNames(filepath.Join(t.TempDir(), "nope.yml"))
	assert.Error(t, err)
}

type composeVarCase struct {
	name string
	app  string
	env  string
	want string
}

func TestComposeVar(t *testing.T) {
	cases := []composeVarCase{
		{"simple app", "worker", "DATABASE_URL", "ULTRA_WORKER__DATABASE_URL"},
		{"other app, same name", "server", "DATABASE_URL", "ULTRA_SERVER__DATABASE_URL"},
		{"hyphen sanitized to underscore", "some-hyphenated-value", "API_KEY", "ULTRA_SOME_HYPHENATED_VALUE__API_KEY"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, ComposeVar(c.app, c.env))
		})
	}
}

type namespaceCase struct {
	name string
	app  string
	want string
}

func TestNamespace(t *testing.T) {
	cases := []namespaceCase{
		{"already an identifier", "worker", "WORKER"},
		{"hyphen becomes underscore", "my-app", "MY_APP"},
		{"underscore is preserved", "my_app", "MY_APP"},
		{"dot becomes underscore", "my.app", "MY_APP"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, Namespace(c.app))
		})
	}
}

// TestNamespaceCollapsesDistinctNames documents that names differing only by
// characters that normalize to the same segment share one namespace, the
// collision gen rejects.
func TestNamespaceCollapsesDistinctNames(t *testing.T) {
	assert.Equal(t, Namespace("my-app"), Namespace("my_app"))
}

type composeCollisionCase struct {
	name    string
	appA    string
	appB    string
	envName string
}

func TestComposeVarNoCollisionAcrossApps(t *testing.T) {
	cases := []composeCollisionCase{
		{"same name in two apps maps to distinct vars", "worker", "server", "DATABASE_URL"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := ComposeVar(c.appA, c.envName)
			b := ComposeVar(c.appB, c.envName)
			assert.NotEqual(t, a, b)
		})
	}
}

type composeOverrideCase struct {
	name       string
	apps       []AppSecrets
	goldenFile string
}

func TestComposeOverride(t *testing.T) {
	cases := []composeOverrideCase{
		{
			name: "combined override over multiple apps matches golden",
			apps: []AppSecrets{
				{App: "worker", Names: []string{"DATABASE_URL", "GOOGLE_CLIENT_ID"}},
				{App: "server", Names: []string{"DATABASE_URL"}},
			},
			goldenFile: "combined_override.golden",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			want, err := os.ReadFile(filepath.Join("..", "testdata", c.goldenFile))
			require.NoError(t, err)
			assert.Equal(t, string(want), ComposeOverride(c.apps))
		})
	}
}
