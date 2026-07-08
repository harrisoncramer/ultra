//go:build integration

// Package integration drives the ultra CLI end to end against a real fixture
// project: a small Go module with an app config package and docker compose
// files. Unlike the unit tests it exercises the parts that shell out — the
// docker-compose config resolver, run's container launch, and validate's
// generated go program — so a break in that wiring is caught. It is behind the
// integration build tag and needs docker for the compose-backed cases.
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var (
	// binPath is the compiled harness CLI, shared across tests.
	binPath string
	// fixtureDir is the tidied fixture module, shared across tests.
	fixtureDir string
)

func TestMain(m *testing.M) {
	os.Exit(runMain(m))
}

// runMain builds the harness and fixture once, then runs the suite. It exists so
// the temp-dir cleanup deferred here isn't skipped by os.Exit in TestMain.
func runMain(m *testing.M) int {
	tmp, err := os.MkdirTemp("", "ultra-it")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tmp)

	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		panic(err)
	}

	binPath = filepath.Join(tmp, "ultra-harness")
	if out, err := build(repoRoot, binPath); err != nil {
		panic("building harness: " + err.Error() + "\n" + out)
	}

	fixtureDir = filepath.Join(tmp, "project")
	if err := setupFixture(repoRoot, fixtureDir); err != nil {
		panic("setting up fixture: " + err.Error())
	}

	return m.Run()
}

// build compiles the integration harness CLI to out.
func build(repoRoot, out string) (string, error) {
	cmd := exec.Command("go", "build", "-tags", "integration", "-o", out, "./test/integration/harness")
	cmd.Dir = repoRoot
	b, err := cmd.CombinedOutput()
	return string(b), err
}

// setupFixture copies the fixture module into dir, points its ultra dependency at
// the local checkout, and tidies it so validate's generated program can build.
func setupFixture(repoRoot, dir string) error {
	if err := copyTree(filepath.Join("testdata", "project"), dir); err != nil {
		return err
	}
	if out, err := runGo(dir, "mod", "edit", "-replace", "github.com/harrisoncramer/ultra="+repoRoot); err != nil {
		return goErr("mod edit", out, err)
	}
	if out, err := runGo(dir, "mod", "tidy"); err != nil {
		return goErr("mod tidy", out, err)
	}
	return nil
}

func TestGenWritesOverrideOffline(t *testing.T) {
	// --override-dir is resolved under --root, so use a relative dir here.
	out, err := ultra(t, "gen", "apps/worker", "--root", fixtureDir, "--override-dir", "genout")
	if err != nil {
		t.Fatalf("gen failed: %v\n%s", err, out)
	}

	path := filepath.Join(fixtureDir, "genout", "worker.compose.yml")
	t.Cleanup(func() { os.RemoveAll(filepath.Join(fixtureDir, "genout")) })
	data, err := os.ReadFile(path)
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

func TestValidatePassesWithCompleteSecrets(t *testing.T) {
	sf := writeSecrets(t, map[string]map[string]string{
		"worker": {"IT_DB_URL": "postgres://it", "IT_API_KEY": "itkey"},
	})
	out, err := ultra(t, "validate", "apps/worker", "--root", fixtureDir, "--secret-resolver", "file", "--secrets-file", sf)
	if err != nil {
		t.Fatalf("validate should pass with all secrets present: %v\n%s", err, out)
	}
	if !strings.Contains(out, "ok") {
		t.Errorf("expected an ok report, got:\n%s", out)
	}
}

func TestValidateFailsOnMissingRequiredSecret(t *testing.T) {
	sf := writeSecrets(t, map[string]map[string]string{
		"worker": {"IT_DB_URL": "postgres://it"}, // IT_API_KEY missing
	})
	out, err := ultra(t, "validate", "apps/worker", "--root", fixtureDir, "--secret-resolver", "file", "--secrets-file", sf)
	if err == nil {
		t.Fatalf("validate should fail when a required secret is missing\n%s", out)
	}
	if !strings.Contains(out, "IT_API_KEY") {
		t.Errorf("expected the failure to name IT_API_KEY, got:\n%s", out)
	}
}

func TestLintFlagsHardcodedSecret(t *testing.T) {
	requireDocker(t)
	sf := writeSecrets(t, map[string]map[string]string{
		"worker": {"IT_DB_URL": "postgres://it", "IT_API_KEY": "itkey"},
	})
	out, err := ultra(t, "lint", "apps/worker", "--root", fixtureDir,
		"--secret-resolver", "file", "--secrets-file", sf,
		"--compose-file", "docker-compose.leak.yml")
	if err == nil {
		t.Fatalf("lint should fail on a hardcoded secret\n%s", out)
	}
	if !strings.Contains(out, "hardcoded") || !strings.Contains(out, "IT_API_KEY") {
		t.Errorf("expected a hardcoded IT_API_KEY finding, got:\n%s", out)
	}
	if strings.Contains(out, "IT_DB_URL") {
		t.Errorf("a ${...} forward should not be flagged, got:\n%s", out)
	}
}

func TestLintPassesWithForwardedSecrets(t *testing.T) {
	requireDocker(t)
	sf := writeSecrets(t, map[string]map[string]string{
		"worker": {"IT_DB_URL": "postgres://it", "IT_API_KEY": "itkey"},
	})
	out, err := ultra(t, "lint", "apps/worker", "--root", fixtureDir,
		"--secret-resolver", "file", "--secrets-file", sf)
	if err != nil {
		t.Fatalf("lint should pass when no secret is hardcoded: %v\n%s", err, out)
	}
}

func TestRunInjectsSecretsIntoContainer(t *testing.T) {
	requireDocker(t)
	t.Cleanup(func() { composeDown(fixtureDir) })

	sf := writeSecrets(t, map[string]map[string]string{
		"worker": {"IT_DB_URL": "postgres://run-it", "IT_API_KEY": "run-secret-value"},
	})
	out, err := ultra(t, "run", "apps/worker", "--root", fixtureDir,
		"--secret-resolver", "file", "--secrets-file", sf,
		"--", "docker", "compose", "up", "--abort-on-container-exit", "--quiet-pull")
	if err != nil {
		t.Fatalf("run failed: %v\n%s", err, out)
	}
	for _, want := range []string{"IT_API_KEY=run-secret-value", "IT_DB_URL=postgres://run-it"} {
		if !strings.Contains(out, want) {
			t.Errorf("container did not observe %q\n%s", want, out)
		}
	}
}

// ultra runs the harness CLI with args and returns its combined output.
func ultra(t *testing.T, args ...string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, binPath, args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.String(), err
}

// writeSecrets writes a JSON secrets file for the file resolver and returns its path.
func writeSecrets(t *testing.T, byApp map[string]map[string]string) string {
	t.Helper()
	data, err := json.Marshal(byApp)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "secrets.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// requireDocker skips the test when docker compose is unavailable.
func requireDocker(t *testing.T) {
	t.Helper()
	if err := exec.Command("docker", "compose", "version").Run(); err != nil {
		t.Skip("docker compose not available")
	}
}

// composeDown tears down the fixture's compose project, ignoring errors.
func composeDown(dir string) {
	cmd := exec.Command("docker", "compose",
		"-f", filepath.Join(dir, "docker-compose.yml"),
		"-f", filepath.Join(dir, "tmp", "worker.compose.yml"),
		"down", "-v")
	_ = cmd.Run()
}

func runGo(dir string, args ...string) (string, error) {
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	b, err := cmd.CombinedOutput()
	return string(b), err
}

func goErr(step, out string, err error) error {
	return &fixtureError{step: step, out: out, err: err}
}

type fixtureError struct {
	step string
	out  string
	err  error
}

func (e *fixtureError) Error() string {
	return e.step + ": " + e.err.Error() + "\n" + e.out
}

// copyTree recursively copies src to dst.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return nil
}
