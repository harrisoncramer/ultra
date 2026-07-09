package configreader

import (
	"errors"
	"testing"

	"github.com/harrisoncramer/ultra/internal/project"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeScanner reports fixed names, or an error, for any config dir.
type fakeScanner struct {
	names []string
	err   error
}

func (f fakeScanner) SecretNames(string) ([]string, error) { return f.names, f.err }

func newTestReader(names []string, err error) *ConfigReader {
	return NewConfigReader(NewConfigReaderParams{
		Scanner: fakeScanner{names: names, err: err},
		Project: project.Project{Root: "/repo", ConfigDir: "config"},
	})
}

func TestReadSortsNames(t *testing.T) {
	out, err := newTestReader([]string{"B", "A", "C"}, nil).Read([]string{"app"})
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, "app", out[0].App)
	assert.Equal(t, []string{"A", "B", "C"}, out[0].Names, "names are sorted for a deterministic override")
}

func TestReadPreservesInputOrder(t *testing.T) {
	out, err := newTestReader([]string{"X"}, nil).Read([]string{"one", "two", "three"})
	require.NoError(t, err)
	got := []string{out[0].App, out[1].App, out[2].App}
	assert.Equal(t, []string{"one", "two", "three"}, got)
}

func TestReadAppWithNoSecrets(t *testing.T) {
	out, err := newTestReader(nil, nil).Read([]string{"app"})
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Empty(t, out[0].Names, "an app that declares no secrets contributes no names")
}

func TestReadPropagatesScannerError(t *testing.T) {
	_, err := newTestReader(nil, errors.New("boom")).Read([]string{"app"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading app config")
}

func TestReadRejectsCollidingNamespaces(t *testing.T) {
	cases := []struct {
		name string
		apps []string
		want string
	}{
		{"same path twice", []string{"apps/worker", "apps/worker"}, "worker"},
		{"different paths, same basename", []string{"apps/worker", "svc/worker"}, "worker"},
		{"colliding only after normalization", []string{"apps/my-app", "apps/my_app"}, "MY_APP"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := newTestReader([]string{"A"}, nil).Read(c.apps)
			require.Error(t, err, "colliding namespaces must not silently cross-contaminate")
			assert.Contains(t, err.Error(), c.want)
		})
	}
}

func TestReadRejectsInvalidEnvName(t *testing.T) {
	cases := [][]string{{"MY-KEY"}, {"my.key"}, {"1KEY"}, {"MY KEY"}}
	for _, names := range cases {
		t.Run(names[0], func(t *testing.T) {
			_, err := newTestReader(names, nil).Read([]string{"app"})
			require.Error(t, err, "a name that breaks compose ${...} interpolation must be rejected")
			assert.Contains(t, err.Error(), names[0])
		})
	}
}
