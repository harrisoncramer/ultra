package scan

import (
	"path/filepath"
	"sort"
	"testing"

	"github.com/caarlos0/env/v10"
	"github.com/harrisoncramer/ultra/internal/testdata/scan/composed"
	"github.com/harrisoncramer/ultra/internal/testdata/scan/crosspkg"
	"github.com/harrisoncramer/ultra/internal/testdata/scan/flat"
	"github.com/harrisoncramer/ultra/internal/testdata/scan/prefixed"
	"github.com/harrisoncramer/ultra/internal/testdata/scan/scoped"
	"github.com/harrisoncramer/ultra/internal/testdata/scan/unexported"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type secretNamesCase struct {
	name    string
	fixture string
	want    []string
	wantErr bool
}

func TestSecretNames(t *testing.T) {
	cases := []secretNamesCase{
		{
			name:    "flat",
			fixture: "flat",
			want:    []string{"SECRET_TOKEN"},
		},
		{
			name:    "embedded and nested",
			fixture: "composed",
			want:    []string{"A_TOKEN", "B_TOKEN", "C_TOKEN"},
		},
		{
			name:    "cross-package sub-struct",
			fixture: "crosspkg",
			want:    []string{"LOCAL_TOKEN", "SUB_TOKEN"},
		},
		{
			name:    "envPrefix stacks, and a reused type gets each prefix",
			fixture: "prefixed",
			want:    []string{"ADDR", "DB_PASSWORD", "REPLICA_PASSWORD", "ROOT_SECRET", "SVC_TOKEN"},
		},
		{
			name:    "a name declared as both secret and non-secret config is rejected",
			fixture: "dupsecret",
			wantErr: true,
		},
		{
			name:    "unexported field is skipped like env.Parse",
			fixture: "unexported",
			want:    []string{"PUBLIC_TOKEN"},
		},
		{
			name:    "no exported Config struct errors",
			fixture: "noconfig",
			wantErr: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := SecretNames(fixtureDir(c.fixture))
			if c.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			sort.Strings(got)
			assert.Equal(t, c.want, got)
		})
	}
}

type fieldCase struct {
	name         string
	fixture      string
	field        string
	wantRequired []string
	wantSecret   bool
}

func TestFields(t *testing.T) {
	cases := []fieldCase{
		{
			name:         "flat optional non-secret",
			fixture:      "flat",
			field:        "PLAIN",
			wantRequired: nil,
			wantSecret:   false,
		},
		{
			name:         "flat required secret",
			fixture:      "flat",
			field:        "SECRET_TOKEN",
			wantRequired: []string{"*"},
			wantSecret:   true,
		},
		{
			name:         "scoped always-required non-secret",
			fixture:      "scoped",
			field:        "ALWAYS",
			wantRequired: []string{"*"},
			wantSecret:   false,
		},
		{
			name:         "scoped always-required secret",
			fixture:      "scoped",
			field:        "API_KEY",
			wantRequired: []string{"*"},
			wantSecret:   true,
		},
		{
			name:         "scoped inherits embedded required env",
			fixture:      "scoped",
			field:        "PROD_TOKEN",
			wantRequired: []string{"production"},
			wantSecret:   true,
		},
		{
			name:         "scoped field-level override wins",
			fixture:      "scoped",
			field:        "OVERRIDE",
			wantRequired: []string{"staging"},
			wantSecret:   false,
		},
		{
			name:         "scoped local-scoped field",
			fixture:      "scoped",
			field:        "LOCAL_URL",
			wantRequired: []string{"local"},
			wantSecret:   false,
		},
		{
			name:         "scoped optional field",
			fixture:      "scoped",
			field:        "OPTIONAL",
			wantRequired: nil,
			wantSecret:   false,
		},
		{
			name:         "prefixed non-secret carries its prefix",
			fixture:      "prefixed",
			field:        "DB_HOST",
			wantRequired: nil,
			wantSecret:   false,
		},
		{
			name:         "reused type under a second prefix",
			fixture:      "prefixed",
			field:        "REPLICA_PASSWORD",
			wantRequired: nil,
			wantSecret:   true,
		},
		{
			name:         "embedded struct stacks its prefix",
			fixture:      "prefixed",
			field:        "SVC_TOKEN",
			wantRequired: nil,
			wantSecret:   true,
		},
		{
			name:         "named nested struct inherits the field's required",
			fixture:      "nestedrequired",
			field:        "DEV_ENDPOINT",
			wantRequired: []string{"local"},
			wantSecret:   false,
		},
		{
			name:         "named nested field-level override wins",
			fixture:      "nestedrequired",
			field:        "DEV_DEBUG",
			wantRequired: []string{"staging"},
			wantSecret:   false,
		},
		{
			name:         "field outside the nested struct is never required",
			fixture:      "nestedrequired",
			field:        "PLAIN",
			wantRequired: nil,
			wantSecret:   false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f, ok := fieldsByName(t, c.fixture)[c.field]
			require.True(t, ok, "%s: field %s not found", c.fixture, c.field)
			assert.Equal(t, c.wantRequired, f.RequiredEnvs)
			assert.Equal(t, c.wantSecret, f.IsSecret)
		})
	}
}

// fixtureDir is the path to a scan fixture package under testdata.
func fixtureDir(fixture string) string {
	return filepath.Join("..", "testdata", "scan", fixture)
}

// fieldsByName type-checks a fixture and indexes its fields by env-var name.
func fieldsByName(t *testing.T, fixture string) map[string]Field {
	t.Helper()
	fields, err := Fields(fixtureDir(fixture))
	require.NoError(t, err)
	byName := make(map[string]Field, len(fields))
	for _, f := range fields {
		byName[f.Name] = f
	}
	return byName
}

type requiredInCase struct {
	name  string
	field string
	env   string
	want  bool
}

func TestFieldRequiredIn(t *testing.T) {
	cases := []requiredInCase{
		{"always required in production", "ALWAYS", "production", true},
		{"always required in local", "ALWAYS", "local", true},
		{"always required in staging", "ALWAYS", "staging", true},
		{"api key required in production", "API_KEY", "production", true},
		{"prod token required in production", "PROD_TOKEN", "production", true},
		{"prod token not required in local", "PROD_TOKEN", "local", false},
		{"prod token not required in staging", "PROD_TOKEN", "staging", false},
		{"override required only in staging", "OVERRIDE", "staging", true},
		{"override not required in production", "OVERRIDE", "production", false},
		{"override not required in local", "OVERRIDE", "local", false},
		{"local url required only in local", "LOCAL_URL", "local", true},
		{"local url not required in production", "LOCAL_URL", "production", false},
		{"optional never required in production", "OPTIONAL", "production", false},
		{"optional never required in local", "OPTIONAL", "local", false},
		{"optional never required in staging", "OPTIONAL", "staging", false},
	}
	byName := fieldsByName(t, "scoped")
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, byName[c.field].RequiredIn(c.env))
		})
	}
}

type fieldsErrorCase struct {
	name    string
	fixture string
}

func TestFieldsRejectsEnvTagOptions(t *testing.T) {
	cases := []fieldsErrorCase{
		{"required/notEmpty in the env tag", "envrequired"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Fields(fixtureDir(c.fixture))
			require.Error(t, err)
		})
	}
}

func TestFieldsRejectsSecretConfigConflict(t *testing.T) {
	_, err := Fields(fixtureDir("dupsecret"))
	require.Error(t, err, "a name declared as both secret and non-secret config must be rejected, not merged")
	assert.Contains(t, err.Error(), "SHARED_URL")
	assert.Contains(t, err.Error(), "both as a secret and as non-secret config")
}

func TestFieldsRejectsSameSourceRedeclaration(t *testing.T) {
	// sharedreq declares SHARED_TOKEN via two fields, both secret, with different
	// required scopes. Rather than silently join the scopes (a hard-to-track bug),
	// the redeclaration is rejected with its own message.
	_, err := Fields(fixtureDir("sharedreq"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SHARED_TOKEN")
	assert.Contains(t, err.Error(), "declared by more than one field")
}

type importPathCase struct {
	name    string
	fixture string
	want    string
}

func TestConfigImportPath(t *testing.T) {
	cases := []importPathCase{
		{"flat fixture import path", "flat", "github.com/harrisoncramer/ultra/internal/testdata/scan/flat"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ConfigImportPath(fixtureDir(c.fixture))
			require.NoError(t, err)
			assert.Equal(t, c.want, got)
		})
	}
}

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

			// Which fields does the caarlos0 library find?
			params, err := env.GetFieldParams(c.cfg)
			require.NoError(t, err)
			want := make(map[string]struct{}, len(params))
			for _, p := range params {
				want[p.Key] = struct{}{}
			}

			// Which fields does our Fields helper find?
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
