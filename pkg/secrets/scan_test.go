package secrets

import (
	"path/filepath"
	"slices"
	"sort"
	"testing"
)

// sortedNames scans a fixture package under testdata and returns its secret env
// names sorted for stable comparison.
func sortedNames(t *testing.T, fixture string) []string {
	t.Helper()
	got, err := SecretNames(filepath.Join("..", "testdata", "scan", fixture))
	if err != nil {
		t.Fatalf("SecretNames(%s): %v", fixture, err)
	}
	sort.Strings(got)
	return got
}

func TestSecretNamesFlat(t *testing.T) {
	if got, want := sortedNames(t, "flat"), []string{"SECRET_TOKEN"}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestSecretNamesEmbeddedAndNested(t *testing.T) {
	if got, want := sortedNames(t, "composed"), []string{"A_TOKEN", "B_TOKEN", "C_TOKEN"}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestFieldsFlagsRequiredAndSecret(t *testing.T) {
	got, err := Fields(filepath.Join("..", "testdata", "scan", "flat"))
	if err != nil {
		t.Fatalf("Fields(flat): %v", err)
	}
	byName := map[string]Field{}
	for _, f := range got {
		byName[f.Name] = f
	}

	plain, ok := byName["PLAIN"]
	if !ok || plain.Required || plain.Secret {
		t.Errorf("PLAIN = %+v, want present, not required, not secret", plain)
	}
	secret, ok := byName["SECRET_TOKEN"]
	if !ok || !secret.Required || !secret.Secret {
		t.Errorf("SECRET_TOKEN = %+v, want present, required (notEmpty), secret", secret)
	}
}

func TestFieldsScope(t *testing.T) {
	fields, err := Fields(filepath.Join("..", "testdata", "scan", "scoped"))
	if err != nil {
		t.Fatalf("Fields(scoped): %v", err)
	}
	byName := map[string]Field{}
	for _, f := range fields {
		byName[f.Name] = f
	}

	cases := []struct {
		name      string
		wantScope []string
		wantSec   bool
	}{
		{"ALWAYS", nil, false},
		{"API_KEY", nil, true},
		{"PROD_TOKEN", []string{"production"}, true}, // inherited from the embedded ProdOnly
		{"OVERRIDE", []string{"staging"}, false},     // field-level override wins
		{"LOCAL_URL", []string{"local"}, false},
		{"OPTIONAL", nil, false},
	}
	for _, c := range cases {
		f, ok := byName[c.name]
		if !ok {
			t.Errorf("%s: not found", c.name)
			continue
		}
		if !slices.Equal(f.Scope, c.wantScope) {
			t.Errorf("%s: scope = %v, want %v", c.name, f.Scope, c.wantScope)
		}
		if f.Secret != c.wantSec {
			t.Errorf("%s: secret = %v, want %v", c.name, f.Secret, c.wantSec)
		}
	}

	// RequiredIn resolves scope against a target environment.
	req := map[string]map[string]bool{
		"production": {"ALWAYS": true, "API_KEY": true, "PROD_TOKEN": true, "OVERRIDE": false, "LOCAL_URL": false, "OPTIONAL": false},
		"local":      {"ALWAYS": true, "API_KEY": true, "PROD_TOKEN": false, "OVERRIDE": false, "LOCAL_URL": true, "OPTIONAL": false},
		"staging":    {"ALWAYS": true, "API_KEY": true, "PROD_TOKEN": false, "OVERRIDE": true, "LOCAL_URL": false, "OPTIONAL": false},
	}
	for env, want := range req {
		for name, w := range want {
			if got := byName[name].RequiredIn(env); got != w {
				t.Errorf("RequiredIn(%q) for %s = %v, want %v", env, name, got, w)
			}
		}
	}
}

func TestSecretNamesCrossPackage(t *testing.T) {
	if got, want := sortedNames(t, "crosspkg"), []string{"LOCAL_TOKEN", "SUB_TOKEN"}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestSecretNamesNoConfig(t *testing.T) {
	if _, err := SecretNames(filepath.Join("..", "testdata", "scan", "noconfig")); err == nil {
		t.Fatal("expected error for a package without an exported Config struct")
	}
}

func TestConfigImportPath(t *testing.T) {
	got, err := ConfigImportPath(filepath.Join("..", "testdata", "scan", "flat"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "github.com/harrisoncramer/ultra/pkg/testdata/scan/flat"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
