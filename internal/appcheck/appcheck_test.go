package appcheck

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
// config value, one secret.
var flatFields = []scan.Field{
	{
		Name: "PLAIN",
	},
	{
		Name:         "SECRET_TOKEN",
		IsSecret:     true,
		RequiredEnvs: []string{"*"},
	},
}

// scopedFields is a set of Config values scoped to specific environments, like
// production or local.
var scopedFields = []scan.Field{
	{
		Name:         "ALWAYS",
		RequiredEnvs: []string{"*"},
	},
	{
		Name:         "API_KEY",
		IsSecret:     true,
		RequiredEnvs: []string{"*"},
	},
	{
		Name:         "PROD_TOKEN",
		IsSecret:     true,
		RequiredEnvs: []string{"production"},
	},
	{
		Name:         "OVERRIDE",
		RequiredEnvs: []string{"staging"},
	},
	{
		Name:         "LOCAL_URL",
		RequiredEnvs: []string{"local"},
	},
	{
		Name: "OPTIONAL",
	},
}

type checkCase struct {
	name               string
	fields             []scan.Field      // The values read from the Config
	environment        string            // The environment the check runs for
	rejectUnreferenced bool              // Whether the reject-unreferenced option is set
	secretVals         map[string]string // The values returned by the secret resolver
	configVals         map[string]string // The values returned by the config resolver
	wantMissing        []string          // The names expected to be reported missing
	wantExtra          []string          // The names expected to be reported unreferenced
}

func TestCheck(t *testing.T) {
	cases := []checkCase{
		{
			name:       "flat passes when the required secret is provided",
			fields:     flatFields,
			secretVals: map[string]string{"SECRET_TOKEN": "placeholder"},
		},
		{
			name:        "flat reports the required secret the store lacks",
			fields:      flatFields,
			wantMissing: []string{"SECRET_TOKEN"},
		},
		{
			name:       "unreferenced keys ignored when reject unreferenced is off",
			fields:     flatFields,
			secretVals: map[string]string{"SECRET_TOKEN": "foo", "STRAY_SECRET": "bar"},
			configVals: map[string]string{"STRAY_CONFIG": "baz"},
		},
		{
			name:               "unreferenced keys reported when reject unreferenced is on",
			fields:             flatFields,
			rejectUnreferenced: true,
			secretVals:         map[string]string{"SECRET_TOKEN": "foo", "STRAY_SECRET": "bar"}, // STRAY_SECRET is not allowed
			configVals:         map[string]string{"PLAIN": "p", "STRAY_CONFIG": "baz"},          // STRAY_CONFIG is not allowed
			wantExtra:          []string{"STRAY_CONFIG", "STRAY_SECRET"},
		},
		{
			name:        "production wants base plus prod-scoped secret",
			fields:      scopedFields,
			environment: "production",
			secretVals:  map[string]string{"API_KEY": "foo", "PROD_TOKEN": "bar"},
			configVals:  map[string]string{"ALWAYS": "baz"},
		},
		{
			name:        "production missing the prod-scoped secret",
			fields:      scopedFields,
			environment: "production",
			secretVals:  map[string]string{"API_KEY": "foo"},
			configVals:  map[string]string{"ALWAYS": "baz"},
			wantMissing: []string{"PROD_TOKEN"},
		},
		{
			name:        "local wants the local-scoped config, not the prod secret",
			fields:      scopedFields,
			environment: "local",
			secretVals:  map[string]string{"API_KEY": "foo"},
			configVals:  map[string]string{"ALWAYS": "baz", "LOCAL_URL": "u"},
		},
		{
			name:        "local missing the local-scoped config",
			fields:      scopedFields,
			environment: "local",
			secretVals:  map[string]string{"API_KEY": "foo"},
			configVals:  map[string]string{"ALWAYS": "baz"},
			wantMissing: []string{"LOCAL_URL"},
		},
		{
			name:        "staging wants the overridden field only",
			fields:      scopedFields,
			environment: "staging",
			secretVals:  map[string]string{"API_KEY": "foo"},
			configVals:  map[string]string{"ALWAYS": "baz", "OVERRIDE": "o"},
		},
		{
			name:        "unscoped required is enforced in every environment",
			fields:      scopedFields,
			environment: "production",
			secretVals:  map[string]string{"API_KEY": "foo", "PROD_TOKEN": "bar"},
			wantMissing: []string{"ALWAYS"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			checker := NewChecker(NewCheckerParams{
				Scanner:            scan.NewFakeConfigScanner(c.fields),
				Project:            project.Project{},
				Environment:        c.environment,
				RejectUnreferenced: c.rejectUnreferenced,
				SecretResolver: func(string) resolve.SecretResolver {
					return resolve.NewFakeSecretResolver(c.secretVals)
				},
				ConfigResolver: resolve.NewFakeConfigResolver(resolve.NewFakeConfigResolverParams{
					Values: c.configVals,
				}),
			})
			r, err := checker.Check(context.Background(), "app")
			require.NoError(t, err)
			assert.Equal(t, c.wantMissing, r.Findings.Missing)
			assert.Equal(t, c.wantExtra, r.Findings.Extra)
			assert.Nil(t, r.Findings.Leaked, "config resolver without leak-checking reports no leaks")
		})
	}
}

func TestCheckFlagsHardcodedSecret(t *testing.T) {
	checker := NewChecker(NewCheckerParams{
		Scanner: scan.NewFakeConfigScanner(flatFields),
		Project: project.Project{},
		SecretResolver: func(string) resolve.SecretResolver {
			return resolve.NewFakeSecretResolver(map[string]string{"SECRET_TOKEN": "foo"})
		},
		ConfigResolver: resolve.NewFakeConfigResolver(resolve.NewFakeConfigResolverParams{
			Values:       map[string]string{"SECRET_TOKEN": "foo"},
			LeakedValues: []string{"SECRET_TOKEN"},
		}),
	})
	r, err := checker.Check(context.Background(), "app")
	require.NoError(t, err)
	assert.Equal(t, []string{"SECRET_TOKEN"}, r.Findings.Leaked)
	assert.Empty(t, r.Findings.Missing, "the secret is provided by the store, just also hardcoded")
}

func TestCheckNoLeakWhenSecretOnlyInStore(t *testing.T) {
	checker := NewChecker(NewCheckerParams{
		Scanner: scan.NewFakeConfigScanner(flatFields),
		Project: project.Project{},
		SecretResolver: func(string) resolve.SecretResolver {
			return resolve.NewFakeSecretResolver(map[string]string{"SECRET_TOKEN": "foo"})
		},
		ConfigResolver: resolve.NewFakeConfigResolver(resolve.NewFakeConfigResolverParams{
			LeakedValues: []string{},
		}),
	})
	r, err := checker.Check(context.Background(), "app")
	require.NoError(t, err)
	assert.Empty(t, r.Findings.Leaked)
}

type problemsCase struct {
	name     string
	findings Findings
	wantOK   bool
	want     []string
}

func TestFindingsProblems(t *testing.T) {
	cases := []problemsCase{
		{
			name:     "no findings is ok and reports nothing",
			findings: Findings{},
			wantOK:   true,
		},
		{
			name:     "each finding renders its own clause",
			findings: Findings{Missing: []string{"A"}, Leaked: []string{"B"}, Extra: []string{"C"}},
			want: []string{
				"missing required keys: A",
				"secrets hardcoded in non-secret config: B",
				"unreferenced keys provided: C",
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.wantOK, c.findings.OK())
			assert.Equal(t, c.want, c.findings.Problems())
		})
	}
}
