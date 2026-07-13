//go:build integration

// Package integration drives the ultra CLI end to end the way a real consumer
// runs it: a binary built with a custom secret resolver that reads from a live
// Redis store, pointed at real fixture projects with docker compose. Unlike the
// unit tests it exercises the parts that shell out (the docker-compose config
// resolver, run's container launch, validate's generated go program) so a break
// in that wiring is caught. It is behind the integration build tag and needs
// docker and the store for every scenario.
package integration

import (
	"os"
	"strings"
	"testing"
)

// TestIntegration builds one rig and runs every scenario against it as a
// subtest. The scenarios share the rig's store and run sequentially, so none of
// them calls t.Parallel.
func TestIntegration(t *testing.T) {
	r := newRig(t)

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
	t.Run("validate redacts a malformed secret value", r.validateRedactsMalformedValue)
	t.Run("run binds a secret under its envPrefix", r.runBindsPrefixedSecret)
	t.Run("validate passes an app with an envPrefix secret", r.validatePassesPrefixedSecret)
	t.Run("validate is not corrupted by a toolchain env var in compose", r.validateIgnoresToolchainEnv)
	t.Run("run skips an app whose name has no matching compose service", r.runSkipsAppNotInCompose)
	t.Run("validate works with a relative root", r.validateWithRelativeRoot)
	t.Run("validate handles a kitchen-sink config", r.validateKitchenSink)
	t.Run("run delivers kitchen-sink secrets into the container", r.runDeliversKitchenSinkSecrets)
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

	// run regenerated the override it injected from; it lists every declared
	// secret name (never a value), so it is safe to leave on disk.
	assertFileContains(t, f.overridePath(), "IT_API_KEY: ${ULTRA_WORKER__IT_API_KEY}")
	assertFileContains(t, f.overridePath(), "IT_DB_URL: ${ULTRA_WORKER__IT_DB_URL}")
}

func (r *Rig) runLeavesUnresolvedSecretEmpty(t *testing.T) {
	s := r.requireStore(t)
	f := r.openFixture(t, "single-app")
	t.Cleanup(func() { r.composeDown(f) })

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

func (r *Rig) runBindsPrefixedSecret(t *testing.T) {
	s := r.requireStore(t)
	f := r.openFixture(t, "prefixed-app")
	t.Cleanup(func() { r.composeDown(f) })

	// The Config nests DB.URL under envPrefix "DB_", so the app reads DB_URL, not
	// URL. The store key and the generated binding must both use the prefixed name.
	if err := s.Seed("web", map[string]string{
		"DB_URL":  "postgres://prefixed",
		"API_KEY": "key-123",
	}); err != nil {
		t.Fatal(err)
	}

	args := append([]string{"run", "apps/web", "--root", f.root}, s.addrFlags()...)
	args = append(args, "--", "docker", "compose", "up", "--abort-on-container-exit", "--quiet-pull")
	res := r.ultra(t, args...)
	if !res.ok() {
		t.Fatalf("run failed:\n%s", res.output)
	}
	// The override binds the prefixed name; before envPrefix was honored it bound
	// the bare URL and the secret never reached the container.
	assertFileContains(t, f.overridePath(), "DB_URL: ${ULTRA_WEB__DB_URL}")
	for _, want := range []string{"DB_URL=postgres://prefixed", "API_KEY=key-123"} {
		if !strings.Contains(res.output, want) {
			t.Errorf("container did not observe %q\n%s", want, res.output)
		}
	}
}

func (r *Rig) validatePassesPrefixedSecret(t *testing.T) {
	s := r.requireStore(t)
	f := r.openFixture(t, "prefixed-app")

	// Both required secrets present under the names the app actually reads.
	if err := s.Seed("web", map[string]string{
		"DB_URL":  "postgres://prefixed",
		"API_KEY": "key-123",
	}); err != nil {
		t.Fatal(err)
	}

	args := append([]string{"validate", "apps/web", "--root", f.root}, s.addrFlags()...)
	res := r.ultra(t, args...)
	if !res.ok() {
		t.Fatalf("validate should pass when the prefixed secrets are present:\n%s", res.output)
	}
	if !strings.Contains(res.output, "ok    web") {
		t.Errorf("expected web to validate ok:\n%s", res.output)
	}
}

func (r *Rig) validateIgnoresToolchainEnv(t *testing.T) {
	s := r.requireStore(t)
	f := r.openFixture(t, "toolchain-app")

	// The app's compose sets GOFLAGS=-mod=vendor as ordinary app config. It must
	// not leak into the environment ultra builds the validation program with, or
	// the go toolchain fails on a nonexistent vendor dir and validate reports a
	// spurious failure that has nothing to do with the app's Config.
	if err := s.Seed("svc", map[string]string{"SVC_TOKEN": "tok"}); err != nil {
		t.Fatal(err)
	}

	args := append([]string{"validate", "apps/svc", "--root", f.root}, s.addrFlags()...)
	res := r.ultra(t, args...)
	if !res.ok() {
		t.Fatalf("a compose toolchain var must not corrupt validate's build:\n%s", res.output)
	}
	if !strings.Contains(res.output, "ok    svc") {
		t.Errorf("expected svc to validate ok:\n%s", res.output)
	}
}

func (r *Rig) runSkipsAppNotInCompose(t *testing.T) {
	s := r.requireStore(t)
	f := r.openFixture(t, "mismatch-app")
	t.Cleanup(func() { r.composeDown(f) })

	// The app dir is apps/frontend but the compose service is "web". Its override
	// block would be an imageless orphan service that makes docker reject the whole
	// project. run must skip it with a clear warning and still bring up the base
	// stack, rather than fail every service with a cryptic docker error.
	if err := s.Seed("frontend", map[string]string{"FE_TOKEN": "tok"}); err != nil {
		t.Fatal(err)
	}

	args := append([]string{"run", "apps/frontend", "--root", f.root}, s.addrFlags()...)
	args = append(args, "--", "docker", "compose", "up", "--abort-on-container-exit", "--quiet-pull")
	res := r.ultra(t, args...)
	if !res.ok() {
		t.Fatalf("run should skip the orphan app and still start the base stack:\n%s", res.output)
	}
	if !strings.Contains(res.output, `has no service "frontend"`) {
		t.Errorf("expected a warning that the app has no matching service:\n%s", res.output)
	}
	if !strings.Contains(res.output, "WEB_RAN") {
		t.Errorf("the base web service should have started:\n%s", res.output)
	}
}

func (r *Rig) validateWithRelativeRoot(t *testing.T) {
	s := r.requireStore(t)
	f := r.openFixture(t, "single-app")

	if err := s.Seed("worker", map[string]string{
		"IT_DB_URL":  "postgres://it",
		"IT_API_KEY": "key",
	}); err != nil {
		t.Fatal(err)
	}

	// Run from inside the fixture with --root ".", the shape the monorepo uses.
	// The generated build dir is then relative, which must not misplace the built
	// validation binary.
	args := append([]string{"validate", "apps/worker", "--root", "."}, s.addrFlags()...)
	res := r.ultraIn(t, f.root, args...)
	if !res.ok() {
		t.Fatalf("validate with a relative root failed:\n%s", res.output)
	}
	if !strings.Contains(res.output, "ok    worker") {
		t.Errorf("expected worker to validate ok:\n%s", res.output)
	}
}

func (r *Rig) validateKitchenSink(t *testing.T) {
	s := r.requireStore(t)

	t.Run("local validates every field kind, nesting and default", func(t *testing.T) {
		s.flush()
		f := r.openFixture(t, "kitchen-sink")
		// Only the always-required secret is needed in local; the production-scoped
		// secrets are not. Every non-secret (URL, ints, floats, bools, the prefixed
		// nested fields, the local-only value) comes from the compose config.
		if err := s.Seed("everything", map[string]string{"API_KEY": "sk-local"}); err != nil {
			t.Fatal(err)
		}
		args := append([]string{"validate", "apps/everything", "--root", f.root}, s.addrFlags()...)
		res := r.ultra(t, args...)
		if !res.ok() {
			t.Fatalf("kitchen-sink config should validate in local:\n%s", res.output)
		}
		if !strings.Contains(res.output, "ok    everything") {
			t.Errorf("expected everything to validate ok:\n%s", res.output)
		}
	})

	t.Run("production enforces star- and production-scoped required, including inherited", func(t *testing.T) {
		s.flush()
		f := r.openFixture(t, "kitchen-sink")
		// Seed only the always-required secret. PROD_SECRET (production-scoped) and
		// OTEL_KEY (production-scoped, inherited from the embedded Telemetry struct)
		// are missing, so production validation must fail naming both.
		if err := s.Seed("everything", map[string]string{"API_KEY": "sk-prod"}); err != nil {
			t.Fatal(err)
		}
		args := append([]string{"validate", "apps/everything", "--root", f.root, "--env", "production"}, s.addrFlags()...)
		res := r.ultra(t, args...)
		if res.ok() {
			t.Fatalf("production validation should fail on the missing production secrets:\n%s", res.output)
		}
		for _, want := range []string{"PROD_SECRET", "OTEL_KEY"} {
			if !strings.Contains(res.output, want) {
				t.Errorf("production failure should name %s:\n%s", want, res.output)
			}
		}
	})
}

func (r *Rig) runDeliversKitchenSinkSecrets(t *testing.T) {
	s := r.requireStore(t)
	f := r.openFixture(t, "kitchen-sink")
	t.Cleanup(func() { r.composeDown(f) })

	// Resolve and inject every declared secret, including OTEL_KEY which lives in
	// the embedded Telemetry struct, and assert each value actually reaches the
	// running container (not merely that validate accepts the config).
	if err := s.Seed("everything", map[string]string{
		"API_KEY":     "sk-run",
		"PROD_SECRET": "prod-run",
		"OTEL_KEY":    "otel-run",
	}); err != nil {
		t.Fatal(err)
	}

	args := append([]string{"run", "apps/everything", "--root", f.root}, s.addrFlags()...)
	args = append(args, "--", "docker", "compose", "up", "--abort-on-container-exit", "--quiet-pull")
	res := r.ultra(t, args...)
	if !res.ok() {
		t.Fatalf("run failed:\n%s", res.output)
	}
	for _, want := range []string{"API_KEY=sk-run", "PROD_SECRET=prod-run", "OTEL_KEY=otel-run"} {
		if !strings.Contains(res.output, want) {
			t.Errorf("container did not observe %q\n%s", want, res.output)
		}
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
