package cli

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mapResolver is a secret or config resolver that returns a fixed set of keys,
// standing in for a real store or a manifest reader.
type mapResolver struct{ have map[string]string }

func (m mapResolver) Resolve(context.Context, []string) (map[string]string, error) {
	return m.have, nil
}

// configMapResolver adapts mapResolver to the ConfigResolver signature.
type configMapResolver struct{ have map[string]string }

func (c configMapResolver) Resolve(context.Context, string) (map[string]string, error) {
	return c.have, nil
}

type lintCase struct {
	name               string
	app                string
	environment        string
	rejectUnreferenced bool
	secretVals         map[string]string
	configVals         map[string]string
	wantMissing        []string
	wantExtra          []string
}

func TestLintApp(t *testing.T) {
	cases := []lintCase{
		{
			// flat declares SECRET_TOKEN (required secret) and PLAIN (optional non-secret).
			name:       "flat passes when the required secret is provided",
			app:        "flat",
			secretVals: map[string]string{"SECRET_TOKEN": "placeholder"},
		},
		{
			name:        "flat reports the required secret the store lacks",
			app:         "flat",
			wantMissing: []string{"SECRET_TOKEN"},
		},
		{
			// Both resolvers hand back a key flat never declares; without the flag
			// those extras are ignored and the app still passes.
			name:       "unreferenced keys ignored when the flag is off",
			app:        "flat",
			secretVals: map[string]string{"SECRET_TOKEN": "x", "STRAY_SECRET": "y"},
			configVals: map[string]string{"STRAY_CONFIG": "z"},
		},
		{
			// flat declares only PLAIN and SECRET_TOKEN, so a stray key from either
			// resolver is unreferenced and reported when the flag is set.
			name:               "unreferenced keys reported when the flag is on",
			app:                "flat",
			rejectUnreferenced: true,
			secretVals:         map[string]string{"SECRET_TOKEN": "x", "STRAY_SECRET": "y"},
			configVals:         map[string]string{"PLAIN": "p", "STRAY_CONFIG": "z"},
			wantExtra:          []string{"STRAY_CONFIG", "STRAY_SECRET"},
		},
		{
			name:        "production wants base plus prod-scoped secret",
			app:         "scoped",
			environment: "production",
			secretVals:  map[string]string{"API_KEY": "x", "PROD_TOKEN": "y"},
			configVals:  map[string]string{"ALWAYS": "z"},
		},
		{
			name:        "production missing the prod-scoped secret",
			app:         "scoped",
			environment: "production",
			secretVals:  map[string]string{"API_KEY": "x"},
			configVals:  map[string]string{"ALWAYS": "z"},
			wantMissing: []string{"PROD_TOKEN"},
		},
		{
			name:        "local wants the local-scoped config, not the prod secret",
			app:         "scoped",
			environment: "local",
			secretVals:  map[string]string{"API_KEY": "x"},
			configVals:  map[string]string{"ALWAYS": "z", "LOCAL_URL": "u"},
		},
		{
			name:        "local missing the local-scoped config",
			app:         "scoped",
			environment: "local",
			secretVals:  map[string]string{"API_KEY": "x"},
			configVals:  map[string]string{"ALWAYS": "z"},
			wantMissing: []string{"LOCAL_URL"},
		},
		{
			name:        "staging wants the overridden field only",
			app:         "scoped",
			environment: "staging",
			secretVals:  map[string]string{"API_KEY": "x"},
			configVals:  map[string]string{"ALWAYS": "z", "OVERRIDE": "o"},
		},
		{
			name:        "unscoped required is enforced in every environment",
			app:         "scoped",
			environment: "production",
			secretVals:  map[string]string{"API_KEY": "x", "PROD_TOKEN": "y"},
			wantMissing: []string{"ALWAYS"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			found, err := lintApp(context.Background(), lintParams{
				root:               filepath.Join("..", "pkg", "testdata", "scan"),
				configDir:          ".",
				environment:        c.environment,
				rejectUnreferenced: c.rejectUnreferenced,
				secretResolver: func(string) SecretResolver {
					return mapResolver{have: c.secretVals}
				},
				configResolver: configMapResolver{have: c.configVals},
			}, c.app)
			require.NoError(t, err)
			assert.Equal(t, c.wantMissing, found.missing)
			assert.Equal(t, c.wantExtra, found.extra)
		})
	}
}
