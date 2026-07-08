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

type fakeScanner struct{ fields []scan.Field }

func (f fakeScanner) Fields(string) ([]scan.Field, error) { return f.fields, nil }

type mapResolver struct{ have map[string]string }

func (m mapResolver) Resolve(context.Context, []string) (map[string]string, error) {
	return m.have, nil
}

type configMapResolver struct{ have map[string]string }

func (c configMapResolver) Resolve(context.Context, string) (map[string]string, error) {
	return c.have, nil
}

// flat declares SECRET_TOKEN (required secret) and PLAIN (optional non-secret).
var flatFields = []scan.Field{
	{Name: "PLAIN"},
	{Name: "SECRET_TOKEN", Secret: true, RequiredEnvs: []string{"*"}},
}

// scoped mirrors the testdata scoped fixture: required tags scoped per env.
var scopedFields = []scan.Field{
	{Name: "ALWAYS", RequiredEnvs: []string{"*"}},
	{Name: "API_KEY", Secret: true, RequiredEnvs: []string{"*"}},
	{Name: "PROD_TOKEN", Secret: true, RequiredEnvs: []string{"production"}},
	{Name: "OVERRIDE", RequiredEnvs: []string{"staging"}},
	{Name: "LOCAL_URL", RequiredEnvs: []string{"local"}},
	{Name: "OPTIONAL"},
}

type lintCase struct {
	name               string
	fields             []scan.Field
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
			name:       "unreferenced keys ignored when the flag is off",
			fields:     flatFields,
			secretVals: map[string]string{"SECRET_TOKEN": "x", "STRAY_SECRET": "y"},
			configVals: map[string]string{"STRAY_CONFIG": "z"},
		},
		{
			name:               "unreferenced keys reported when the flag is on",
			fields:             flatFields,
			rejectUnreferenced: true,
			secretVals:         map[string]string{"SECRET_TOKEN": "x", "STRAY_SECRET": "y"},
			configVals:         map[string]string{"PLAIN": "p", "STRAY_CONFIG": "z"},
			wantExtra:          []string{"STRAY_CONFIG", "STRAY_SECRET"},
		},
		{
			name:        "production wants base plus prod-scoped secret",
			fields:      scopedFields,
			environment: "production",
			secretVals:  map[string]string{"API_KEY": "x", "PROD_TOKEN": "y"},
			configVals:  map[string]string{"ALWAYS": "z"},
		},
		{
			name:        "production missing the prod-scoped secret",
			fields:      scopedFields,
			environment: "production",
			secretVals:  map[string]string{"API_KEY": "x"},
			configVals:  map[string]string{"ALWAYS": "z"},
			wantMissing: []string{"PROD_TOKEN"},
		},
		{
			name:        "local wants the local-scoped config, not the prod secret",
			fields:      scopedFields,
			environment: "local",
			secretVals:  map[string]string{"API_KEY": "x"},
			configVals:  map[string]string{"ALWAYS": "z", "LOCAL_URL": "u"},
		},
		{
			name:        "local missing the local-scoped config",
			fields:      scopedFields,
			environment: "local",
			secretVals:  map[string]string{"API_KEY": "x"},
			configVals:  map[string]string{"ALWAYS": "z"},
			wantMissing: []string{"LOCAL_URL"},
		},
		{
			name:        "staging wants the overridden field only",
			fields:      scopedFields,
			environment: "staging",
			secretVals:  map[string]string{"API_KEY": "x"},
			configVals:  map[string]string{"ALWAYS": "z", "OVERRIDE": "o"},
		},
		{
			name:        "unscoped required is enforced in every environment",
			fields:      scopedFields,
			environment: "production",
			secretVals:  map[string]string{"API_KEY": "x", "PROD_TOKEN": "y"},
			wantMissing: []string{"ALWAYS"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			l := NewLinter(NewLinterParams{
				Scanner:            fakeScanner{fields: c.fields},
				Project:            project.Project{},
				Environment:        c.environment,
				RejectUnreferenced: c.rejectUnreferenced,
				SecretResolver:     func(string) resolve.SecretResolver { return mapResolver{have: c.secretVals} },
				ConfigResolver:     configMapResolver{have: c.configVals},
			})
			found, err := l.checkApp(context.Background(), "app")
			require.NoError(t, err)
			assert.Equal(t, c.wantMissing, found.missing)
			assert.Equal(t, c.wantExtra, found.extra)
		})
	}
}
