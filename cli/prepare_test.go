package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubResolver returns a fixed set of secret values, ignoring the requested names,
// so a test can simulate a store that holds only a subset of an app's secrets.
type stubResolver struct{ values map[string]string }

func (s stubResolver) Resolve(ctx context.Context, names []string) (map[string]string, error) {
	return s.values, nil
}

// writeTestApp writes a throwaway Go module with a config package declaring the
// given secret-tagged env names, and returns the repo root it lives under.
func writeTestApp(t *testing.T, names ...string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "app", "config")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "app", "go.mod"), []byte("module testapp\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	b.WriteString("package config\n\ntype Config struct {\n")
	for _, n := range names {
		b.WriteString("\t" + n + " string `env:\"" + n + "\" secret:\"true\"`\n")
	}
	b.WriteString("}\n")
	if err := os.WriteFile(filepath.Join(dir, "config.go"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestPrepareOmitsUnresolvedSecrets(t *testing.T) {
	root := writeTestApp(t, "RESOLVED", "MISSING")
	p := runParams{
		root:      root,
		apps:      []string{"app"},
		configDir: "config",
		resolverFor: func(app string) SecretResolver {
			return stubResolver{values: map[string]string{"RESOLVED": "value"}}
		},
	}

	prep, err := prepare(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}

	override := filepath.Join(root, "tmp", "app.compose.yml")
	data, err := os.ReadFile(override)
	if err != nil {
		t.Fatalf("reading override: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "RESOLVED:") {
		t.Errorf("override missing resolved secret RESOLVED:\n%s", got)
	}
	if strings.Contains(got, "MISSING:") {
		t.Errorf("override references unresolved secret MISSING, would clobber platform value:\n%s", got)
	}

	if !hasEnv(prep.env, "ULTRA_APP__RESOLVED", "value") {
		t.Errorf("launcher env missing ULTRA_APP__RESOLVED=value")
	}
	for _, e := range prep.env {
		if strings.HasPrefix(e, "ULTRA_APP__MISSING=") {
			t.Errorf("launcher env forwarded unresolved secret: %q", e)
		}
	}
}

func TestPrepareWritesNoOverrideWhenNothingResolves(t *testing.T) {
	root := writeTestApp(t, "MISSING")
	p := runParams{
		root:      root,
		apps:      []string{"app"},
		configDir: "config",
		resolverFor: func(app string) SecretResolver {
			return stubResolver{values: map[string]string{}}
		},
	}

	prep, err := prepare(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(root, "tmp", "app.compose.yml")); !os.IsNotExist(err) {
		t.Errorf("expected no override file when no secrets resolve, stat err = %v", err)
	}
	for _, f := range prep.composeFiles {
		if strings.HasSuffix(f, "app.compose.yml") {
			t.Errorf("composeFiles includes an override that was never written: %q", f)
		}
	}
}

func hasEnv(env []string, key, val string) bool {
	want := key + "=" + val
	for _, e := range env {
		if e == want {
			return true
		}
	}
	return false
}
