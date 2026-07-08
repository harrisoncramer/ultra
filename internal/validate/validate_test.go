package validate

import (
	"context"
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
