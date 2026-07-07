package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

type appConfigDirCase struct {
	name      string
	root      string
	appPath   string
	configDir string
	want      string
}

func TestAppConfigDir(t *testing.T) {
	cases := []appConfigDirCase{
		{"default config dir", ".", "apps/worker", "config", "apps/worker/config"},
		{"empty config dir defaults to config", ".", "apps/worker", "", "apps/worker/config"},
		{"nested config dir", ".", "apps/axle", "pkg/config", "apps/axle/pkg/config"},
		{"anchored under root", "/repo", "apps/axle", "pkg/config", "/repo/apps/axle/pkg/config"},
		{"absolute app path ignores root", "/repo", "/abs/axle", "pkg/config", "/abs/axle/pkg/config"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, appConfigDir(c.root, c.appPath, c.configDir))
		})
	}
}

type appNameCase struct {
	name    string
	appPath string
	want    string
}

func TestAppName(t *testing.T) {
	cases := []appNameCase{
		{"relative path", "apps/worker", "worker"},
		{"another relative path", "apps/axle", "axle"},
		{"absolute path", "/repo/apps/axle", "axle"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, appName(c.appPath))
		})
	}
}
