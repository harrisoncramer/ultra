package cli

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type validateCase struct {
	name               string
	app                string
	rejectUnreferenced bool
	secretVals         map[string]string
	configVals         map[string]string
	wantErr            bool
	wantContains       []string
}

func TestValidateRejectsUnreferenced(t *testing.T) {
	cases := []validateCase{
		{
			// flat declares only PLAIN and SECRET_TOKEN. Both resolvers hand back an
			// extra key, so the unreferenced check fails the app before it ever tries
			// to build and run the config-loading program.
			name:               "extra keys from both resolvers fail the app",
			app:                "flat",
			rejectUnreferenced: true,
			secretVals:         map[string]string{"SECRET_TOKEN": "x", "STRAY_SECRET": "y"},
			configVals:         map[string]string{"STRAY_CONFIG": "z"},
			wantErr:            true,
			wantContains:       []string{"STRAY_SECRET", "STRAY_CONFIG"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateApp(context.Background(), validateParams{
				root:               filepath.Join("..", "pkg", "testdata", "scan"),
				configDir:          ".",
				rejectUnreferenced: c.rejectUnreferenced,
				secretResolver: func(string) SecretResolver {
					return mapResolver{have: c.secretVals}
				},
				configResolver: configMapResolver{have: c.configVals},
			}, c.app)
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
	name       string
	secretVals map[string]string
	msg        string
	// wantAbsent must not appear in the redacted output.
	wantAbsent []string
	// wantPresent must appear in the redacted output.
	wantPresent []string
	// wantUnchanged asserts the message is returned verbatim.
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
