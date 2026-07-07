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
