//go:build integration

// Package integration drives the ultra CLI end to end the way a real consumer
// runs it: a binary built with a custom secret resolver that reads from a live
// Redis store, pointed at real fixture projects with docker compose. Unlike the
// unit tests it exercises the parts that shell out — the docker-compose config
// resolver, run's container launch, validate's generated go program — so a break
// in that wiring is caught. It is behind the integration build tag and needs
// docker for every scenario except gen.
package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestIntegration builds one rig and runs every scenario against it as a
// subtest. The scenarios share the rig's store and run sequentially, so none of
// them calls t.Parallel.
func TestIntegration(t *testing.T) {
	r := newRig(t)

	t.Run("gen lists all declared secrets offline", r.genListsAllDeclaredSecrets)
	t.Run("run injects resolved secrets into a container", r.runInjectsResolvedSecrets)
	t.Run("run leaves an unresolved secret empty", r.runLeavesUnresolvedSecretEmpty)
	t.Run("run namespaces a shared secret per app", r.runNamespacesSecretsPerApp)
	t.Run("validate passes complete and fails on a missing secret", r.validatePassesAndFails)
	t.Run("validate enforces env-scoped required", r.validateEnvScopedRequired)
	t.Run("lint flags a hardcoded secret", r.lintFlagsHardcodedSecret)
}

func (r *Rig) genListsAllDeclaredSecrets(t *testing.T) {
	// gen needs neither docker nor the store: it is a static scan of the config.
	f := r.openFixture(t, "single-app")
	res := r.ultra(t, "gen", "apps/worker", "--root", f.root, "--override-dir", "genout")
	if !res.ok() {
		t.Fatalf("gen failed:\n%s", res.output)
	}

	data, err := os.ReadFile(filepath.Join(f.root, "genout", "worker.compose.yml"))
	if err != nil {
		t.Fatalf("reading override: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		"IT_API_KEY: ${ULTRA_WORKER__IT_API_KEY}",
		"IT_DB_URL: ${ULTRA_WORKER__IT_DB_URL}",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("override missing %q\n%s", want, got)
		}
	}
}

func (r *Rig) runInjectsResolvedSecrets(t *testing.T) {
	s := r.requireStore(t)
	f := r.openFixture(t, "single-app")
	t.Cleanup(func() { r.composeDown(f, "worker") })

	if err := s.Seed("worker", map[string]string{
		"IT_DB_URL":  "postgres://run-it",
		"IT_API_KEY": "run-secret-value",
	}); err != nil {
		t.Fatal(err)
	}

	args := append([]string{"run", "apps/worker", "--root", f.root}, s.addrFlags()...)
	args = append(args, "--", "docker", "compose", "up", "--abort-on-container-exit", "--quiet-pull")
	res := r.ultra(t, args...)
	if !res.ok() {
		t.Fatalf("run failed:\n%s", res.output)
	}
	for _, want := range []string{"IT_DB_URL=postgres://run-it", "IT_API_KEY=run-secret-value"} {
		if !strings.Contains(res.output, want) {
			t.Errorf("container did not observe %q\n%s", want, res.output)
		}
	}
}

func (r *Rig) runLeavesUnresolvedSecretEmpty(t *testing.T) {
	s := r.requireStore(t)
	f := r.openFixture(t, "single-app")
	t.Cleanup(func() { r.composeDown(f, "worker") })

	// Only one of the two declared secrets is in the store.
	if err := s.Seed("worker", map[string]string{"IT_DB_URL": "postgres://only-db"}); err != nil {
		t.Fatal(err)
	}

	args := append([]string{"run", "apps/worker", "--root", f.root}, s.addrFlags()...)
	args = append(args, "--", "docker", "compose", "up", "--abort-on-container-exit", "--quiet-pull")
	res := r.ultra(t, args...)
	if !res.ok() {
		t.Fatalf("run failed:\n%s", res.output)
	}
	if !strings.Contains(res.output, "IT_DB_URL=postgres://only-db") {
		t.Errorf("resolved secret not injected\n%s", res.output)
	}
	// The unresolved secret's override entry interpolates to empty, so the echoed
	// line ends right after IT_API_KEY=.
	if !strings.Contains(res.output, "IT_API_KEY=\n") {
		t.Errorf("unresolved secret should be empty, not carry a value\n%s", res.output)
	}
}

func (r *Rig) runNamespacesSecretsPerApp(t *testing.T) {
	s := r.requireStore(t)
	f := r.openFixture(t, "multi-app")

	if err := s.Seed("server", map[string]string{"DATABASE_URL": "srv-db", "SERVER_TOKEN": "st"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Seed("worker", map[string]string{"DATABASE_URL": "wrk-db", "WORKER_TOKEN": "wt"}); err != nil {
		t.Fatal(err)
	}

	args := append([]string{"run", "apps/server", "apps/worker", "--root", f.root}, s.addrFlags()...)
	args = append(args, "--", "true")
	res := r.ultra(t, args...)
	if !res.ok() {
		t.Fatalf("run failed:\n%s", res.output)
	}

	// The shared name DATABASE_URL is namespaced per app, so the two overrides map
	// it onto distinct launcher variables and can't collide.
	assertFileContains(t, f.overridePath("server"), "DATABASE_URL: ${ULTRA_SERVER__DATABASE_URL}")
	assertFileContains(t, f.overridePath("worker"), "DATABASE_URL: ${ULTRA_WORKER__DATABASE_URL}")
	for _, want := range []string{"resolved 2/2 secrets for server", "resolved 2/2 secrets for worker"} {
		if !strings.Contains(res.output, want) {
			t.Errorf("expected %q\n%s", want, res.output)
		}
	}
}

func (r *Rig) validatePassesAndFails(t *testing.T) {
	s := r.requireStore(t)
	f := r.openFixture(t, "single-app")
	args := append([]string{"validate", "apps/worker", "--root", f.root}, s.addrFlags()...)

	if err := s.Seed("worker", map[string]string{"IT_DB_URL": "postgres://it", "IT_API_KEY": "k"}); err != nil {
		t.Fatal(err)
	}
	if res := r.ultra(t, args...); !res.ok() {
		t.Fatalf("validate should pass with all secrets present:\n%s", res.output)
	}

	s.flush()
	if err := s.Seed("worker", map[string]string{"IT_DB_URL": "postgres://it"}); err != nil {
		t.Fatal(err)
	}
	res := r.ultra(t, args...)
	if res.ok() {
		t.Fatalf("validate should fail when a required secret is missing:\n%s", res.output)
	}
	if !strings.Contains(res.output, "IT_API_KEY") {
		t.Errorf("failure should name IT_API_KEY:\n%s", res.output)
	}
}

func (r *Rig) validateEnvScopedRequired(t *testing.T) {
	s := r.requireStore(t)
	f := r.openFixture(t, "scoped-envs")
	base := append([]string{"validate", "apps/api", "--root", f.root}, s.addrFlags()...)

	t.Run("production enforces the prod-scoped secret", func(t *testing.T) {
		s.flush()
		if err := s.Seed("api", map[string]string{"IT_ALWAYS": "a"}); err != nil {
			t.Fatal(err)
		}
		res := r.ultra(t, append(base, "--env", "production")...)
		if res.ok() {
			t.Fatalf("expected failure for missing IT_PROD:\n%s", res.output)
		}
		if !strings.Contains(res.output, "IT_PROD") {
			t.Errorf("failure should name IT_PROD:\n%s", res.output)
		}
	})

	t.Run("local does not enforce the prod-scoped secret", func(t *testing.T) {
		s.flush()
		if err := s.Seed("api", map[string]string{"IT_ALWAYS": "a", "IT_LOCAL": "l"}); err != nil {
			t.Fatal(err)
		}
		if res := r.ultra(t, append(base, "--env", "local")...); !res.ok() {
			t.Fatalf("local should not require IT_PROD:\n%s", res.output)
		}
	})
}

func (r *Rig) lintFlagsHardcodedSecret(t *testing.T) {
	s := r.requireStore(t)
	f := r.openFixture(t, "single-app")
	if err := s.Seed("worker", map[string]string{"IT_DB_URL": "postgres://it", "IT_API_KEY": "k"}); err != nil {
		t.Fatal(err)
	}
	args := append([]string{"lint", "apps/worker", "--root", f.root}, s.addrFlags()...)

	if clean := r.ultra(t, args...); !clean.ok() {
		t.Fatalf("lint should pass on the clean compose file:\n%s", clean.output)
	}

	leak := r.ultra(t, append(args, "--compose-file", "docker-compose.leak.yml")...)
	if leak.ok() {
		t.Fatalf("lint should fail on a hardcoded secret:\n%s", leak.output)
	}
	if !strings.Contains(leak.output, "hardcoded") || !strings.Contains(leak.output, "IT_API_KEY") {
		t.Errorf("expected a hardcoded IT_API_KEY finding:\n%s", leak.output)
	}
	if strings.Contains(leak.output, "IT_DB_URL") {
		t.Errorf("a ${...} forward must not be flagged:\n%s", leak.output)
	}
}

func assertFileContains(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	if !strings.Contains(string(data), want) {
		t.Errorf("%s missing %q\n%s", path, want, data)
	}
}
