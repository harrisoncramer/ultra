package secrets

import (
	"testing"

	"github.com/caarlos0/env/v10"
	"github.com/harrisoncramer/ultra/pkg/testdata/scan/composed"
	"github.com/harrisoncramer/ultra/pkg/testdata/scan/crosspkg"
	"github.com/harrisoncramer/ultra/pkg/testdata/scan/flat"
	"github.com/harrisoncramer/ultra/pkg/testdata/scan/prefixed"
	"github.com/harrisoncramer/ultra/pkg/testdata/scan/scoped"
	"github.com/harrisoncramer/ultra/pkg/testdata/scan/unexported"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFieldsMatchesEnvGetFieldParams pins the static scanner to caarlos0/env's
// own view of a Config. Fields reads env tags with go/types so it can enumerate
// names without building the app; env.GetFieldParams reads the same tags via
// reflection at runtime. For every fixture the set of env names must agree, so
// any drift in how ultra parses env tags or resolves envPrefix (exactly where
// past scanner bugs lived) fails here rather than silently binding the wrong
// variable. These fixtures declare each name once; env.GetFieldParams returns one
// entry per field, so its keys are compared as a set.
func TestFieldsMatchesEnvGetFieldParams(t *testing.T) {
	cases := []struct {
		fixture string
		cfg     any
	}{
		{"flat", &flat.Config{}},
		{"composed", &composed.Config{}},
		{"crosspkg", &crosspkg.Config{}},
		{"prefixed", &prefixed.Config{}},
		{"scoped", &scoped.Config{}},
		{"unexported", &unexported.Config{}},
	}
	for _, c := range cases {
		t.Run(c.fixture, func(t *testing.T) {
			params, err := env.GetFieldParams(c.cfg)
			require.NoError(t, err)
			want := make(map[string]struct{}, len(params))
			for _, p := range params {
				want[p.Key] = struct{}{}
			}

			got, err := Fields(fixtureDir(c.fixture))
			require.NoError(t, err)
			have := make(map[string]struct{}, len(got))
			for _, f := range got {
				have[f.Name] = struct{}{}
			}

			assert.Equal(t, want, have, "the static scan and caarlos0/env must agree on env names")
		})
	}
}
