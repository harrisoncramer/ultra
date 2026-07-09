//go:build integration

// Package integration drives the ultra CLI end to end the way a real consumer
// runs it: a binary built with a custom secret resolver that reads from a live
// Redis store, pointed at real fixture projects with docker compose. Unlike the
// unit tests it exercises the parts that shell out (the docker-compose config
// resolver, run's container launch, validate's generated go program) so a break
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
	t.Run("validate coerces non-string env values", r.validateCoercesScalarEnv)
	t.Run("validate enforces env-scoped required", r.validateEnvScopedRequired)
	t.Run("lint flags a hardcoded secret", r.lintFlagsHardcodedSecret)
	t.Run("validate flags a hardcoded secret", r.validateFlagsHardcodedSecret)
	t.Run("run injects a value with special characters verbatim", r.runInjectsSpecialCharValue)
	t.Run("run forwards an empty secret value", r.runForwardsEmptySecretValue)
	t.Run("run skips an app that declares no secrets", r.runSkipsAppWithoutSecrets)
	t.Run("run rejects a stale override", r.runRejectsStaleOverride)
	t.Run("validate redacts a malformed secret value", r.validateRedactsMalformedValue)
	t.Run("gen honors a custom config dir", r.genHonorsConfigDir)
	t.Run("config file supplies defaults the command line overrides", r.configFilePrecedence)
}

func (r *Rig) genListsAllDeclaredSecrets(t *testing.T) {
	// gen needs neither docker nor the store: it is a static scan of the config.
	f := r.openFixture(t, "single-app")
	res := r.ultra(t, "gen", "apps/worker", "--root", f.root, "--output-dir", "genout")
	if !res.ok() {
		t.Fatalf("gen failed:\n%s", res.output)
	}

	data, err := os.ReadFile(filepath.Join(f.root, "genout", overrideName))
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
	t.Cleanup(func() { r.composeDown(f) })

	if err := s.Seed("worker", map[string]string{
		"IT_DB_URL":  "postgres://run-it",
		"IT_API_KEY": "run-secret-value",
	}); err != nil {
		t.Fatal(err)
	}

	r.genOverride(t, f, "apps/worker")

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
	t.Cleanup(func() { r.composeDown(f) })

	// Only one of the two declared secrets is in the store.
	if err := s.Seed("worker", map[string]string{"IT_DB_URL": "postgres://only-db"}); err != nil {
		t.Fatal(err)
	}

	r.genOverride(t, f, "apps/worker")

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

	r.genOverride(t, f, "apps/server", "apps/worker")

	args := append([]string{"run", "apps/server", "apps/worker", "--root", f.root}, s.addrFlags()...)
	args = append(args, "--", "true")
	res := r.ultra(t, args...)
	if !res.ok() {
		t.Fatalf("run failed:\n%s", res.output)
	}

	// The shared name DATABASE_URL is namespaced per app, so within the one
	// override the two service blocks map it onto distinct launcher variables and
	// can't collide.
	assertFileContains(t, f.overridePath(), "DATABASE_URL: ${ULTRA_SERVER__DATABASE_URL}")
	assertFileContains(t, f.overridePath(), "DATABASE_URL: ${ULTRA_WORKER__DATABASE_URL}")
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

func (r *Rig) validateCoercesScalarEnv(t *testing.T) {
	s := r.requireStore(t)
	f := r.openFixture(t, "single-app")
	if err := s.Seed("worker", map[string]string{"IT_DB_URL": "postgres://it", "IT_API_KEY": "k"}); err != nil {
		t.Fatal(err)
	}

	// single-app's compose sets non-string env values (OTEL_ENABLED: false,
	// OTEL_SAMPLE_RATE: 1). docker compose config emits those as a JSON bool and
	// number, so the config resolver must coerce them to strings rather than fail
	// to parse the environment.
	args := append([]string{"validate", "apps/worker", "--root", f.root}, s.addrFlags()...)
	res := r.ultra(t, args...)
	if !res.ok() {
		t.Fatalf("validate must coerce scalar env values, not fail parsing:\n%s", res.output)
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

func (r *Rig) validateFlagsHardcodedSecret(t *testing.T) {
	s := r.requireStore(t)
	f := r.openFixture(t, "single-app")
	if err := s.Seed("worker", map[string]string{"IT_DB_URL": "postgres://it", "IT_API_KEY": "k"}); err != nil {
		t.Fatal(err)
	}
	args := append([]string{"validate", "apps/worker", "--root", f.root}, s.addrFlags()...)

	// The clean compose file hardcodes no secret, so validate passes.
	if clean := r.ultra(t, args...); !clean.ok() {
		t.Fatalf("validate should pass on the clean compose file:\n%s", clean.output)
	}

	// The leak file pastes IT_API_KEY as a literal, so validate must fail the same
	// way lint does; a ${...} forward (IT_DB_URL) is not a hardcoded value.
	leak := r.ultra(t, append(args, "--compose-file", "docker-compose.leak.yml")...)
	if leak.ok() {
		t.Fatalf("validate should fail on a hardcoded secret:\n%s", leak.output)
	}
	if !strings.Contains(leak.output, "hardcoded") || !strings.Contains(leak.output, "IT_API_KEY") {
		t.Errorf("expected a hardcoded IT_API_KEY finding:\n%s", leak.output)
	}
	if strings.Contains(leak.output, "IT_DB_URL") {
		t.Errorf("a ${...} forward must not be flagged:\n%s", leak.output)
	}
}

func (r *Rig) runInjectsSpecialCharValue(t *testing.T) {
	s := r.requireStore(t)
	f := r.openFixture(t, "single-app")
	t.Cleanup(func() { r.composeDown(f) })

	// A value laden with compose- and shell-significant characters must reach the
	// container untouched: no re-interpolation of $VAR/${VAR}, no word splitting.
	const gnarly = `p@ss w0rd$X-${Y}="z"`
	if err := s.Seed("worker", map[string]string{
		"IT_DB_URL":  "postgres://it",
		"IT_API_KEY": gnarly,
	}); err != nil {
		t.Fatal(err)
	}

	r.genOverride(t, f, "apps/worker")

	args := append([]string{"run", "apps/worker", "--root", f.root}, s.addrFlags()...)
	args = append(args, "--", "docker", "compose", "up", "--abort-on-container-exit", "--quiet-pull")
	res := r.ultra(t, args...)
	if !res.ok() {
		t.Fatalf("run failed:\n%s", res.output)
	}
	if !strings.Contains(res.output, "IT_API_KEY="+gnarly) {
		t.Errorf("special-char value was not delivered verbatim\n%s", res.output)
	}
}

func (r *Rig) runForwardsEmptySecretValue(t *testing.T) {
	s := r.requireStore(t)
	f := r.openFixture(t, "single-app")
	t.Cleanup(func() { r.composeDown(f) })

	// The store holds the key with an empty value, distinct from not holding it.
	if err := s.Seed("worker", map[string]string{"IT_DB_URL": "postgres://it", "IT_API_KEY": ""}); err != nil {
		t.Fatal(err)
	}

	r.genOverride(t, f, "apps/worker")

	args := append([]string{"run", "apps/worker", "--root", f.root}, s.addrFlags()...)
	args = append(args, "--", "docker", "compose", "up", "--abort-on-container-exit", "--quiet-pull")
	res := r.ultra(t, args...)
	if !res.ok() {
		t.Fatalf("run failed:\n%s", res.output)
	}
	// An empty value is still a resolved secret, so both count, and the container
	// sees IT_API_KEY as empty.
	if !strings.Contains(res.output, "resolved 2/2 secrets for worker") {
		t.Errorf("an empty value should count as resolved\n%s", res.output)
	}
	if !strings.Contains(res.output, "IT_API_KEY=\n") {
		t.Errorf("empty secret should reach the container as empty\n%s", res.output)
	}
}

func (r *Rig) runSkipsAppWithoutSecrets(t *testing.T) {
	s := r.requireStore(t)
	f := r.openFixture(t, "multi-app")

	if err := s.Seed("server", map[string]string{"DATABASE_URL": "srv", "SERVER_TOKEN": "st"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Seed("worker", map[string]string{"DATABASE_URL": "wrk", "WORKER_TOKEN": "wt"}); err != nil {
		t.Fatal(err)
	}

	r.genOverride(t, f, "apps/server", "apps/worker", "apps/nosec")

	args := append([]string{"run", "apps/server", "apps/worker", "apps/nosec", "--root", f.root}, s.addrFlags()...)
	args = append(args, "--", "true")
	res := r.ultra(t, args...)
	if !res.ok() {
		t.Fatalf("run failed:\n%s", res.output)
	}

	// nosec declares no secrets, so it contributes no service block; the others
	// still do.
	assertFileContains(t, f.overridePath(), "DATABASE_URL: ${ULTRA_SERVER__DATABASE_URL}")
	assertFileContains(t, f.overridePath(), "DATABASE_URL: ${ULTRA_WORKER__DATABASE_URL}")
	if data, err := os.ReadFile(f.overridePath()); err != nil {
		t.Errorf("reading combined override: %v", err)
	} else if strings.Contains(string(data), "nosec:") {
		t.Errorf("an app with no secrets should get no service block:\n%s", data)
	}
}

func (r *Rig) runRejectsStaleOverride(t *testing.T) {
	s := r.requireStore(t)
	f := r.openFixture(t, "multi-app")

	if err := s.Seed("worker", map[string]string{"DATABASE_URL": "wrk", "WORKER_TOKEN": "wt"}); err != nil {
		t.Fatal(err)
	}

	// gen only covers server, so the committed override carries no bindings for
	// worker. Running worker against it must fail: worker's secrets would resolve
	// but never reach the container, exactly the drift the fingerprint guards.
	r.genOverride(t, f, "apps/server")

	args := append([]string{"run", "apps/worker", "--root", f.root}, s.addrFlags()...)
	args = append(args, "--", "true")
	res := r.ultra(t, args...)
	if res.ok() {
		t.Fatalf("run should reject an override that doesn't cover worker:\n%s", res.output)
	}
	if !strings.Contains(res.output, "stale") || !strings.Contains(res.output, "worker") {
		t.Errorf("expected a stale-override error naming worker:\n%s", res.output)
	}
}

func (r *Rig) validateRedactsMalformedValue(t *testing.T) {
	s := r.requireStore(t)
	f := r.openFixture(t, "malformed")

	// IT_PORT is an int field, so a non-numeric value fails parsing; the value is
	// a secret, so it must be redacted from the reported error.
	if err := s.Seed("app", map[string]string{"IT_TOKEN": "tok", "IT_PORT": "not-a-number"}); err != nil {
		t.Fatal(err)
	}

	args := append([]string{"validate", "apps/app", "--root", f.root}, s.addrFlags()...)
	res := r.ultra(t, args...)
	if res.ok() {
		t.Fatalf("validate should fail on a malformed value:\n%s", res.output)
	}
	if strings.Contains(res.output, "not-a-number") {
		t.Errorf("the malformed secret value leaked into the error:\n%s", res.output)
	}
	if !strings.Contains(res.output, "[redacted]") {
		t.Errorf("expected the value to be redacted:\n%s", res.output)
	}
}

func (r *Rig) genHonorsConfigDir(t *testing.T) {
	// gen is a static scan, so this needs neither docker nor the store.
	f := r.openFixture(t, "custom-config-dir")

	ok := r.ultra(t, "gen", "apps/worker", "--root", f.root, "--config-dir", "pkg/config", "--output-dir", "out")
	if !ok.ok() {
		t.Fatalf("gen should find the config under pkg/config:\n%s", ok.output)
	}
	assertFileContains(t, filepath.Join(f.root, "out", overrideName), "IT_API_KEY: ${ULTRA_WORKER__IT_API_KEY}")

	// The default config dir is "config", which this fixture doesn't have, so
	// gen must fail without the flag.
	if bad := r.ultra(t, "gen", "apps/worker", "--root", f.root, "--output-dir", "out2"); bad.ok() {
		t.Errorf("gen should fail when the config dir is wrong:\n%s", bad.output)
	}
}

func (r *Rig) configFilePrecedence(t *testing.T) {
	// gen is a static scan, so this needs neither docker nor the store.
	f := r.openFixture(t, "config-file")

	// No app args and no --output-dir: both come from .ultra.toml.
	if res := r.ultra(t, "gen", "--root", f.root); !res.ok() {
		t.Fatalf("gen should use apps and output-dir from .ultra.toml:\n%s", res.output)
	}
	assertFileContains(t, filepath.Join(f.root, "from-file", overrideName), "IT_API_KEY:")

	// A command-line --output-dir wins over the file's value.
	if res := r.ultra(t, "gen", "--root", f.root, "--output-dir", "from-cli"); !res.ok() {
		t.Fatalf("gen with a command-line output-dir failed:\n%s", res.output)
	}
	if _, err := os.Stat(filepath.Join(f.root, "from-cli", overrideName)); err != nil {
		t.Errorf("command-line --output-dir should win over the file: %v", err)
	}
}

// genOverride runs gen for the given apps into the fixture's default output
// dir, so run has the committed override to point COMPOSE_FILE at. run no
// longer generates the override itself; gen and run are separate steps.
func (r *Rig) genOverride(t *testing.T, f *fixture, apps ...string) {
	t.Helper()
	args := append([]string{"gen"}, apps...)
	args = append(args, "--root", f.root)
	if res := r.ultra(t, args...); !res.ok() {
		t.Fatalf("gen failed:\n%s", res.output)
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
