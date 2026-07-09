//go:build integration

package integration

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// fixtureNames are the testdata projects materialized once per rig.
var fixtureNames = []string{
	"single-app",
	"multi-app",
	"scoped-envs",
	"malformed",
	"custom-config-dir",
	"config-file",
	"prefixed-app",
	"toolchain-app",
	"mismatch-app",
}

// Rig holds the shared, expensive setup the scenarios run against: the compiled
// harness binary, the tidied fixture templates, and, when docker is present,
// the live secret store. It is built once per run and injected into each
// scenario, so there is no package-level state.
type Rig struct {
	repoRoot  string
	binPath   string
	store     *redisStore
	templates map[string]string
}

// newRig builds the harness and fixture templates, and starts the store when
// docker is available, registering its teardown with t.
func newRig(t *testing.T) *Rig {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolving repo root: %v", err)
	}
	work := t.TempDir()

	r := &Rig{repoRoot: root, templates: map[string]string{}}

	r.binPath = filepath.Join(work, "ultra-harness")
	if out, err := buildHarness(root, r.binPath); err != nil {
		t.Fatalf("building harness: %v\n%s", err, out)
	}

	for _, name := range fixtureNames {
		dir := filepath.Join(work, "templates", name)
		if err := prepareTemplate(root, name, dir); err != nil {
			t.Fatalf("preparing fixture %s: %v", name, err)
		}
		r.templates[name] = dir
	}

	if dockerReady() {
		store, err := startRedisStore()
		if err != nil {
			t.Fatalf("starting store: %v", err)
		}
		t.Cleanup(store.Close)
		r.store = store
	}
	return r
}

// ultra runs the harness CLI with args and captures its combined output and exit
// code.
func (r *Rig) ultra(t *testing.T, args ...string) result {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, r.binPath, args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()

	res := result{output: buf.String()}
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		res.exit = 0
	case errors.As(err, &exitErr):
		res.exit = exitErr.ExitCode()
	default:
		t.Fatalf("running harness %v: %v\n%s", args, err, res.output)
	}
	return res
}

// openFixture materializes a fresh, writable copy of the named fixture template.
func (r *Rig) openFixture(t *testing.T, name string) *fixture {
	t.Helper()
	dir, ok := r.templates[name]
	if !ok {
		t.Fatalf("unknown fixture %q", name)
	}
	return newFixture(t, dir)
}

// requireStore returns the store, skipping the scenario when it isn't up and
// flushing it so no key leaks in from a prior one. Scenarios share the one store
// and run sequentially, so they must not call t.Parallel.
func (r *Rig) requireStore(t *testing.T) *redisStore {
	t.Helper()
	if r.store == nil {
		t.Skip("docker/store not available")
	}
	r.store.flush()
	return r.store
}

// composeDown tears down a fixture's compose project after a run brings it up.
// The base file alone identifies the project (same dir, same project name), so
// the generated override isn't needed to stop it.
func (r *Rig) composeDown(f *fixture) {
	_ = exec.Command("docker", "compose",
		"-f", filepath.Join(f.root, "docker-compose.yml"),
		"down", "-v", "--remove-orphans").Run()
}

// buildHarness compiles the integration harness CLI to out.
func buildHarness(repoRoot, out string) (string, error) {
	cmd := exec.Command("go", "build", "-tags", "integration", "-o", out, "./test/integration/harness")
	cmd.Dir = repoRoot
	b, err := cmd.CombinedOutput()
	return string(b), err
}

func dockerReady() bool {
	return exec.Command("docker", "compose", "version").Run() == nil
}

// result is the outcome of one harness CLI invocation.
type result struct {
	output string
	exit   int
}

// ok reports whether the command exited zero.
func (r result) ok() bool { return r.exit == 0 }
