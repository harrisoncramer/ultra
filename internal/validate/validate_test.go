package validate

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/harrisoncramer/ultra/internal/project"
	"github.com/harrisoncramer/ultra/internal/resolve"
	"github.com/harrisoncramer/ultra/internal/scan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeScanner reports fixed fields for any dir.
type fakeScanner struct{ fields []scan.Field }

func (f fakeScanner) Fields(string) ([]scan.Field, error)     { return f.fields, nil }
func (f fakeScanner) ConfigImportPath(string) (string, error) { return "example.com/x", nil }

type mapResolver struct{ have map[string]string }

func (m mapResolver) Resolve(context.Context, []string) (map[string]string, error) {
	return m.have, nil
}

type configMapResolver struct{ have map[string]string }

func (c configMapResolver) Resolve(context.Context, string) (map[string]string, error) {
	return c.have, nil
}

// leakConfigResolver is a config resolver that also reports hardcoded secrets,
// standing in for docker-compose's SecretLeakChecker capability.
type leakConfigResolver struct {
	have   map[string]string
	leaked []string
}

func (c leakConfigResolver) Resolve(context.Context, string) (map[string]string, error) {
	return c.have, nil
}

func (c leakConfigResolver) LeakedSecrets(context.Context, string, []string) ([]string, error) {
	return c.leaked, nil
}

// flat declares only PLAIN (non-secret) and SECRET_TOKEN (secret).
var flatFields = []scan.Field{
	{Name: "PLAIN"},
	{Name: "SECRET_TOKEN", Secret: true},
}

type validateCase struct {
	name         string
	secretVals   map[string]string
	configVals   map[string]string
	wantErr      bool
	wantContains []string
}

func TestValidateRejectsUnreferenced(t *testing.T) {
	cases := []validateCase{
		{
			// Both resolvers hand back an extra key, so the unreferenced check fails
			// the app before it ever tries to build and run the config-loading program.
			name:         "extra keys from both resolvers fail the app",
			secretVals:   map[string]string{"SECRET_TOKEN": "x", "STRAY_SECRET": "y"},
			configVals:   map[string]string{"STRAY_CONFIG": "z"},
			wantErr:      true,
			wantContains: []string{"STRAY_SECRET", "STRAY_CONFIG"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := NewValidator(NewValidatorParams{
				Scanner:            fakeScanner{fields: flatFields},
				Project:            project.Project{},
				RejectUnreferenced: true,
				SecretResolver:     func(string) resolve.SecretResolver { return mapResolver{have: c.secretVals} },
				ConfigResolver:     configMapResolver{have: c.configVals},
			})
			err := v.validateApp(context.Background(), "flat")
			if !c.wantErr {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			for _, want := range c.wantContains {
				assert.ErrorContains(t, err, want)
			}
		})
	}
}

func TestValidateRejectsHardcodedSecret(t *testing.T) {
	cases := []struct {
		name    string
		leaked  []string
		wantErr bool
		want    string
	}{
		{name: "hardcoded secret fails the app", leaked: []string{"SECRET_TOKEN"}, wantErr: true, want: "SECRET_TOKEN"},
		{name: "no leak passes the leak check", leaked: nil, wantErr: false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := NewValidator(NewValidatorParams{
				Scanner:        fakeScanner{fields: flatFields},
				Project:        project.Project{},
				SecretResolver: func(string) resolve.SecretResolver { return mapResolver{have: map[string]string{"SECRET_TOKEN": "x"}} },
				ConfigResolver: leakConfigResolver{leaked: c.leaked},
			})
			err := v.validateApp(context.Background(), "flat")
			if !c.wantErr {
				// With no leak the leak check passes; the app then builds and runs a
				// program against a bogus import path, so any remaining error must not
				// be the hardcoded-secret one.
				if err != nil {
					assert.NotContains(t, err.Error(), "hardcoded")
				}
				return
			}
			require.Error(t, err)
			assert.ErrorContains(t, err, "hardcoded")
			assert.ErrorContains(t, err, c.want)
		})
	}
}

func TestValidateRefusesToClobberExistingDir(t *testing.T) {
	root := t.TempDir()
	// The app parent already holds an ultravalidate directory with user content;
	// validate must not touch, let alone delete, it.
	genDir := filepath.Join(root, "app", "ultravalidate")
	require.NoError(t, os.MkdirAll(genDir, 0o755))
	sentinel := filepath.Join(genDir, "important.txt")
	require.NoError(t, os.WriteFile(sentinel, []byte("precious"), 0o644))

	v := NewValidator(NewValidatorParams{
		// No secrets, so the store is never hit and validateApp reaches the dir
		// check without a resolver.
		Scanner:        fakeScanner{fields: []scan.Field{{Name: "PLAIN"}}},
		Project:        project.Project{Root: root},
		SecretResolver: func(string) resolve.SecretResolver { return mapResolver{} },
		ConfigResolver: configMapResolver{have: map[string]string{}},
	})

	err := v.validateApp(context.Background(), "app")
	require.Error(t, err)
	assert.ErrorContains(t, err, "already exists")

	data, readErr := os.ReadFile(sentinel)
	require.NoError(t, readErr, "the pre-existing directory must be left intact")
	assert.Equal(t, "precious", string(data))
}

type redactCase struct {
	name          string
	secretVals    map[string]string
	msg           string
	wantAbsent    []string
	wantPresent   []string
	wantUnchanged bool
}

func TestRedactSecrets(t *testing.T) {
	cases := []redactCase{
		{
			name:        "masks a leaked value and inserts the placeholder",
			secretVals:  map[string]string{"DB_POOL_SIZE": "s3cr3t-oops", "API_TOKEN": "abc123", "EMPTY": ""},
			msg:         `parse error on field "PoolSize" of type "int": strconv.ParseInt: parsing "s3cr3t-oops": invalid syntax`,
			wantAbsent:  []string{"s3cr3t-oops"},
			wantPresent: []string{"[redacted]"},
		},
		{
			name:       "masks the longer value whole when one contains another",
			secretVals: map[string]string{"SHORT": "abc", "LONG": "abc123"},
			msg:        `bad values abc123 and abc here`,
			wantAbsent: []string{"abc123"},
		},
		{
			name:          "nil map leaves the message unchanged",
			secretVals:    nil,
			msg:           "some error",
			wantUnchanged: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := redactSecrets(c.msg, c.secretVals)
			if c.wantUnchanged {
				assert.Equal(t, c.msg, got)
			}
			for _, s := range c.wantAbsent {
				assert.NotContains(t, got, s)
			}
			for _, s := range c.wantPresent {
				assert.Contains(t, got, s)
			}
		})
	}
}
