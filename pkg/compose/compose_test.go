package compose_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/harrisoncramer/ultra/pkg/compose"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
		{"hyphen sanitized to underscore", "dafpay-network", "API_KEY", "ULTRA_DAFPAY_NETWORK__API_KEY"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, compose.ComposeVar(c.app, c.env))
		})
	}
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
			a := compose.ComposeVar(c.appA, c.envName)
			b := compose.ComposeVar(c.appB, c.envName)
			assert.NotEqual(t, a, b)
		})
	}
}

type composeOverrideCase struct {
	name       string
	app        string
	envNames   []string
	goldenFile string
}

func TestComposeOverride(t *testing.T) {
	cases := []composeOverrideCase{
		{
			name:       "worker override matches golden",
			app:        "worker",
			envNames:   []string{"DATABASE_URL", "GOOGLE_CLIENT_ID"},
			goldenFile: "worker_override.golden",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			want, err := os.ReadFile(filepath.Join("..", "testdata", c.goldenFile))
			require.NoError(t, err)
			assert.Equal(t, string(want), compose.ComposeOverride(c.app, c.envNames))
		})
	}
}
