package cli

import (
	"testing"

	"github.com/harrisoncramer/ultra/internal/project"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type resolveAppsCase struct {
	name string
	args []string
	apps []string
	want []string
}

func TestResolveApps(t *testing.T) {
	cases := []resolveAppsCase{
		{
			name: "command-line args win over the file",
			args: []string{"apps/server"},
			apps: []string{"apps/worker"},
			want: []string{"apps/server"},
		},
		{
			name: "comma-joined args are split and trimmed",
			args: []string{"apps/server, apps/worker"},
			want: []string{"apps/server", "apps/worker"},
		},
		{
			name: "file apps are used when no args are given",
			apps: []string{"apps/worker"},
			want: []string{"apps/worker"},
		},
		{
			name: "blank and whitespace file entries are dropped",
			apps: []string{"apps/worker", "", "  "},
			want: []string{"apps/worker"},
		},
		{
			name: "comma-joined file entry is split like args",
			apps: []string{"apps/server,apps/worker"},
			want: []string{"apps/server", "apps/worker"},
		},
		{
			name: "no args and no file yields nothing",
			want: nil,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := resolveApps(c.args, fileConfig{apps: c.apps})
			assert.Equal(t, c.want, got)
		})
	}
}

type collisionCase struct {
	name    string
	apps    []string
	wantErr bool
	want    string
}

func TestAssertNoAppCollisions(t *testing.T) {
	cases := []collisionCase{
		{name: "distinct names pass", apps: []string{"apps/server", "apps/worker"}},
		{name: "same path twice collides", apps: []string{"apps/worker", "apps/worker"}, wantErr: true, want: "worker"},
		{name: "same basename collides", apps: []string{"apps/worker", "svc/worker"}, wantErr: true, want: "worker"},
		{name: "collision only after normalization", apps: []string{"apps/my-app", "apps/my_app"}, wantErr: true, want: "MY_APP"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := assertNoAppCollisions(c.apps, project.Project{})
			if !c.wantErr {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), c.want)
		})
	}
}
