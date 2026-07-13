package lint

import (
	"context"
	"testing"

	"github.com/harrisoncramer/ultra/internal/project"
	"github.com/harrisoncramer/ultra/internal/resolve"
	"github.com/harrisoncramer/ultra/internal/scan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// flatFields is a set of Config values declared at the top level -- one plain
// config value, one required secret.
var flatFields = []scan.Field{
	{Name: "PLAIN"},
	{Name: "SECRET_TOKEN", IsSecret: true, RequiredEnvs: []string{"*"}},
}

// newLinter builds a Linter whose resolvers hand back the given values. The
// per-app static checks themselves are covered in the appcheck package; these
// tests exercise only how Lint aggregates and reports across apps.
func newLinter(secretVals map[string]string) *Linter {
	return NewLinter(NewLinterParams{
		Scanner: scan.NewFakeConfigScanner(flatFields),
		Project: project.Project{},
		SecretResolver: func(string) resolve.SecretResolver {
			return resolve.NewFakeSecretResolver(secretVals)
		},
		ConfigResolver: resolve.NewFakeConfigResolver(resolve.NewFakeConfigResolverParams{}),
	})
}

func TestLintPassesWhenEveryAppIsComplete(t *testing.T) {
	l := newLinter(map[string]string{"SECRET_TOKEN": "provided"})
	require.NoError(t, l.Lint(context.Background(), []string{"one", "two"}))
}

func TestLintErrorsAndCountsFailingApps(t *testing.T) {
	// Neither app's required secret is provided, so both fail and the error names
	// the count.
	l := newLinter(nil)
	err := l.Lint(context.Background(), []string{"one", "two"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "2 app(s) failed lint")
}
