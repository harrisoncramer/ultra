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
