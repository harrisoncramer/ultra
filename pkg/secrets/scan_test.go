package secrets

import (
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

type secretNamesCase struct {
	name    string
	fixture string
	want    []string
	wantErr bool
}

func TestSecretNames(t *testing.T) {
	cases := []secretNamesCase{
		{name: "flat", fixture: "flat", want: []string{"SECRET_TOKEN"}},
		{name: "embedded and nested", fixture: "composed", want: []string{"A_TOKEN", "B_TOKEN", "C_TOKEN"}},
		{name: "cross-package sub-struct", fixture: "crosspkg", want: []string{"LOCAL_TOKEN", "SUB_TOKEN"}},
		{name: "envPrefix stacks, and a reused type gets each prefix", fixture: "prefixed", want: []string{"ADDR", "DB_PASSWORD", "REPLICA_PASSWORD", "ROOT_SECRET", "SVC_TOKEN"}},
		{name: "duplicate env name keeps the secret flag regardless of visit order", fixture: "dupsecret", want: []string{"SHARED_URL"}},
		{name: "unexported field is skipped like env.Parse", fixture: "unexported", want: []string{"PUBLIC_TOKEN"}},
		{name: "no exported Config struct errors", fixture: "noconfig", wantErr: true},
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
		{"flat optional non-secret", "flat", "PLAIN", nil, false},
		{"flat required secret", "flat", "SECRET_TOKEN", []string{"*"}, true},
		{"scoped always-required non-secret", "scoped", "ALWAYS", []string{"*"}, false},
		{"scoped always-required secret", "scoped", "API_KEY", []string{"*"}, true},
		{"scoped inherits embedded required env", "scoped", "PROD_TOKEN", []string{"production"}, true},
		{"scoped field-level override wins", "scoped", "OVERRIDE", []string{"staging"}, false},
		{"scoped local-scoped field", "scoped", "LOCAL_URL", []string{"local"}, false},
		{"scoped optional field", "scoped", "OPTIONAL", nil, false},
		{"prefixed non-secret carries its prefix", "prefixed", "DB_HOST", nil, false},
		{"reused type under a second prefix", "prefixed", "REPLICA_PASSWORD", nil, true},
		{"embedded struct stacks its prefix", "prefixed", "SVC_TOKEN", nil, true},
		{"duplicate name merges to secret", "dupsecret", "SHARED_URL", nil, true},
		{"shared struct unions required scopes", "sharedreq", "SHARED_TOKEN", []string{"production"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f, ok := fieldsByName(t, c.fixture)[c.field]
			require.True(t, ok, "%s: field %s not found", c.fixture, c.field)
			assert.Equal(t, c.wantRequired, f.RequiredEnvs)
			assert.Equal(t, c.wantSecret, f.Secret)
		})
	}
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

type importPathCase struct {
	name    string
	fixture string
	want    string
}

func TestConfigImportPath(t *testing.T) {
	cases := []importPathCase{
		{"flat fixture import path", "flat", "github.com/harrisoncramer/ultra/pkg/testdata/scan/flat"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ConfigImportPath(fixtureDir(c.fixture))
			require.NoError(t, err)
			assert.Equal(t, c.want, got)
		})
	}
}
