//go:build integration

// Package integration drives the ultra CLI end to end the way a real consumer
// runs it: a binary built with a custom secret resolver that reads from a live
// Redis store, pointed at real fixture projects with docker compose. Unlike the
// unit tests it exercises the parts that shell out — the docker-compose config
// resolver, run's container launch, validate's generated go program — so a break
// in that wiring is caught. It is behind the integration build tag and needs
// docker for every case except gen.
package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var (
	repoRoot  string
	binPath   string
	templates = map[string]string{}
	store     *redisStore
)

// fixtures materialized once as tidied templates, then copied per test.
var fixtureNames = []string{"single-app", "multi-app", "scoped-envs"}

func TestMain(m *testing.M) {
	os.Exit(runMain(m))
}

// runMain builds the harness and fixture templates once and, when docker is
// present, starts the store. It exists so the deferred cleanup isn't skipped by
// os.Exit in TestMain.
func runMain(m *testing.M) int {
	tmp, err := os.MkdirTemp("", "ultra-it")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tmp)

	if repoRoot, err = filepath.Abs(filepath.Join("..", "..")); err != nil {
		panic(err)
	}

	binPath = filepath.Join(tmp, "ultra-harness")
	if out, err := buildHarness(binPath); err != nil {
		panic("building harness: " + err.Error() + "\n" + out)
	}

	for _, name := range fixtureNames {
		dir := filepath.Join(tmp, "templates", name)
		if err := prepareTemplate(name, dir); err != nil {
			panic("preparing fixture " + name + ": " + err.Error())
		}
		templates[name] = dir
	}

	if dockerReady() {
		store, err = startRedisStore()
		if err != nil {
			panic("starting store: " + err.Error())
		}
		defer store.Close()
	}

	return m.Run()
}

func buildHarness(out string) (string, error) {
	cmd := exec.Command("go", "build", "-tags", "integration", "-o", out, "./test/integration/harness")
	cmd.Dir = repoRoot
	b, err := cmd.CombinedOutput()
	return string(b), err
}

func dockerReady() bool {
	return exec.Command("docker", "compose", "version").Run() == nil
}

// requireStore skips a test when the store isn't up, and flushes it so no key
// leaks in from a prior scenario.
func requireStore(t *testing.T) *redisStore {
	t.Helper()
	if store == nil {
		t.Skip("docker/store not available")
	}
	store.flush()
	return store
}

// composeDown tears down an app's compose project after a run brings it up.
func composeDown(f *fixture, app string) {
	_ = exec.Command("docker", "compose",
		"-f", filepath.Join(f.root, "docker-compose.yml"),
		"-f", f.overridePath(app),
		"down", "-v", "--remove-orphans").Run()
}

func TestGenListsAllDeclaredSecretsOffline(t *testing.T) {
	// gen needs neither docker nor the store: it is a static scan of the config.
	f := openFixture(t, "single-app")
	res := ultra(t, "gen", "apps/worker", "--root", f.root, "--override-dir", "genout")
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

func TestRunInjectsResolvedSecrets(t *testing.T) {
	s := requireStore(t)
	f := openFixture(t, "single-app")
	t.Cleanup(func() { composeDown(f, "worker") })

	if err := s.Seed("worker", map[string]string{
		"IT_DB_URL":  "postgres://run-it",
		"IT_API_KEY": "run-secret-value",
	}); err != nil {
		t.Fatal(err)
	}

	args := append([]string{"run", "apps/worker", "--root", f.root}, s.addrFlags()...)
	args = append(args, "--", "docker", "compose", "up", "--abort-on-container-exit", "--quiet-pull")
	res := ultra(t, args...)
	if !res.ok() {
		t.Fatalf("run failed:\n%s", res.output)
	}
	for _, want := range []string{"IT_DB_URL=postgres://run-it", "IT_API_KEY=run-secret-value"} {
		if !strings.Contains(res.output, want) {
			t.Errorf("container did not observe %q\n%s", want, res.output)
		}
	}
}

func TestRunLeavesUnresolvedSecretEmpty(t *testing.T) {
	s := requireStore(t)
	f := openFixture(t, "single-app")
	t.Cleanup(func() { composeDown(f, "worker") })

	// Only one of the two declared secrets is in the store.
	if err := s.Seed("worker", map[string]string{"IT_DB_URL": "postgres://only-db"}); err != nil {
		t.Fatal(err)
	}

	args := append([]string{"run", "apps/worker", "--root", f.root}, s.addrFlags()...)
	args = append(args, "--", "docker", "compose", "up", "--abort-on-container-exit", "--quiet-pull")
	res := ultra(t, args...)
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

func TestRunNamespacesSecretsPerApp(t *testing.T) {
	s := requireStore(t)
	f := openFixture(t, "multi-app")

	if err := s.Seed("server", map[string]string{"DATABASE_URL": "srv-db", "SERVER_TOKEN": "st"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Seed("worker", map[string]string{"DATABASE_URL": "wrk-db", "WORKER_TOKEN": "wt"}); err != nil {
		t.Fatal(err)
	}

	args := append([]string{"run", "apps/server", "apps/worker", "--root", f.root}, s.addrFlags()...)
	args = append(args, "--", "true")
	res := ultra(t, args...)
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

func TestValidatePassesAndFails(t *testing.T) {
	s := requireStore(t)
	f := openFixture(t, "single-app")

	if err := s.Seed("worker", map[string]string{"IT_DB_URL": "postgres://it", "IT_API_KEY": "k"}); err != nil {
		t.Fatal(err)
	}
	args := append([]string{"validate", "apps/worker", "--root", f.root}, s.addrFlags()...)
	if res := ultra(t, args...); !res.ok() {
		t.Fatalf("validate should pass with all secrets present:\n%s", res.output)
	}

	s.flush()
	if err := s.Seed("worker", map[string]string{"IT_DB_URL": "postgres://it"}); err != nil {
		t.Fatal(err)
	}
	res := ultra(t, args...)
	if res.ok() {
		t.Fatalf("validate should fail when a required secret is missing:\n%s", res.output)
	}
	if !strings.Contains(res.output, "IT_API_KEY") {
		t.Errorf("failure should name IT_API_KEY:\n%s", res.output)
	}
}

func TestValidateEnvScopedRequired(t *testing.T) {
	s := requireStore(t)
	f := openFixture(t, "scoped-envs")
	base := append([]string{"validate", "apps/api", "--root", f.root}, s.addrFlags()...)

	t.Run("production enforces the prod-scoped secret", func(t *testing.T) {
		s.flush()
		if err := s.Seed("api", map[string]string{"IT_ALWAYS": "a"}); err != nil {
			t.Fatal(err)
		}
		res := ultra(t, append(base, "--env", "production")...)
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
		if res := ultra(t, append(base, "--env", "local")...); !res.ok() {
			t.Fatalf("local should not require IT_PROD:\n%s", res.output)
		}
	})
}

func TestLintFlagsHardcodedSecret(t *testing.T) {
	s := requireStore(t)
	f := openFixture(t, "single-app")
	if err := s.Seed("worker", map[string]string{"IT_DB_URL": "postgres://it", "IT_API_KEY": "k"}); err != nil {
		t.Fatal(err)
	}

	args := append([]string{"lint", "apps/worker", "--root", f.root}, s.addrFlags()...)

	clean := ultra(t, args...)
	if !clean.ok() {
		t.Fatalf("lint should pass on the clean compose file:\n%s", clean.output)
	}

	leak := ultra(t, append(args, "--compose-file", "docker-compose.leak.yml")...)
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
