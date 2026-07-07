package cli

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
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

func lintFlat(t *testing.T, secretVals, configVals map[string]string) []string {
	t.Helper()
	missing, err := lintApp(context.Background(), lintParams{
		root:      filepath.Join("..", "pkg", "testdata", "scan"),
		configDir: ".",
		secretResolver: func(string) SecretResolver {
			return mapResolver{have: secretVals}
		},
		configResolver: configMapResolver{have: configVals},
	}, "flat")
	if err != nil {
		t.Fatalf("lintApp(flat): %v", err)
	}
	return missing
}

func TestLintPassesWhenRequiredKeyProvided(t *testing.T) {
	// flat declares SECRET_TOKEN (required secret) and PLAIN (optional non-secret).
	missing := lintFlat(t, map[string]string{"SECRET_TOKEN": "placeholder"}, nil)
	if len(missing) != 0 {
		t.Fatalf("missing = %v, want none", missing)
	}
}

func TestLintReportsMissingRequiredSecret(t *testing.T) {
	// The store provides nothing, so the required SECRET_TOKEN is unprovided.
	missing := lintFlat(t, nil, nil)
	if want := []string{"SECRET_TOKEN"}; !slices.Equal(missing, want) {
		t.Fatalf("missing = %v, want %v", missing, want)
	}
}

func TestLintScopesByEnvironment(t *testing.T) {
	cases := []struct {
		name        string
		environment string
		secretVals  map[string]string
		configVals  map[string]string
		want        []string
	}{
		{
			name:        "production wants base plus prod-scoped secret",
			environment: "production",
			secretVals:  map[string]string{"API_KEY": "x", "PROD_TOKEN": "y"},
			configVals:  map[string]string{"ALWAYS": "z"},
			want:        nil,
		},
		{
			name:        "production missing the prod-scoped secret",
			environment: "production",
			secretVals:  map[string]string{"API_KEY": "x"},
			configVals:  map[string]string{"ALWAYS": "z"},
			want:        []string{"PROD_TOKEN"},
		},
		{
			name:        "local wants the local-scoped config, not the prod secret",
			environment: "local",
			secretVals:  map[string]string{"API_KEY": "x"},
			configVals:  map[string]string{"ALWAYS": "z", "LOCAL_URL": "u"},
			want:        nil,
		},
		{
			name:        "local missing the local-scoped config",
			environment: "local",
			secretVals:  map[string]string{"API_KEY": "x"},
			configVals:  map[string]string{"ALWAYS": "z"},
			want:        []string{"LOCAL_URL"},
		},
		{
			name:        "staging wants the overridden field only",
			environment: "staging",
			secretVals:  map[string]string{"API_KEY": "x"},
			configVals:  map[string]string{"ALWAYS": "z", "OVERRIDE": "o"},
			want:        nil,
		},
		{
			name:        "unscoped required is enforced in every environment",
			environment: "production",
			secretVals:  map[string]string{"API_KEY": "x", "PROD_TOKEN": "y"},
			configVals:  nil,
			want:        []string{"ALWAYS"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			missing, err := lintApp(context.Background(), lintParams{
				root:        filepath.Join("..", "pkg", "testdata", "scan"),
				configDir:   ".",
				environment: c.environment,
				secretResolver: func(string) SecretResolver {
					return mapResolver{have: c.secretVals}
				},
				configResolver: configMapResolver{have: c.configVals},
			}, "scoped")
			if err != nil {
				t.Fatalf("lintApp(scoped): %v", err)
			}
			if !slices.Equal(missing, c.want) {
				t.Fatalf("missing = %v, want %v", missing, c.want)
			}
		})
	}
}
