//go:build integration

package integration

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// fixture is a materialized copy of a testdata project: a real Go module ultra
// can scan, validate and run against.
type fixture struct {
	root string
}

// openFixture copies a prepared template of the named testdata project into a
// fresh temp dir, so each test gets its own writable tree (overrides, generated
// programs) without disturbing others.
func openFixture(t *testing.T, name string) *fixture {
	t.Helper()
	tmpl, ok := templates[name]
	if !ok {
		t.Fatalf("unknown fixture %q", name)
	}
	root := filepath.Join(t.TempDir(), name)
	if err := copyTree(tmpl, root); err != nil {
		t.Fatalf("copying fixture: %v", err)
	}
	return &fixture{root: root}
}

// overridePath is where run/gen write app's override under the default dir.
func (f *fixture) overridePath(app string) string {
	return filepath.Join(f.root, "tmp", app+".compose.yml")
}

// prepareTemplate copies a testdata project into dir, points its ultra
// dependency at the local checkout, and tidies it once so per-test copies don't
// each pay for a module resolution.
func prepareTemplate(name, dir string) error {
	if err := copyTree(filepath.Join("testdata", name), dir); err != nil {
		return err
	}
	if out, err := runGo(dir, "mod", "edit", "-replace", "github.com/harrisoncramer/ultra="+repoRoot); err != nil {
		return fmt.Errorf("mod edit %s: %w\n%s", name, err, out)
	}
	if out, err := runGo(dir, "mod", "tidy"); err != nil {
		return fmt.Errorf("mod tidy %s: %w\n%s", name, err, out)
	}
	return nil
}

func runGo(dir string, args ...string) (string, error) {
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	b, err := cmd.CombinedOutput()
	return string(b), err
}

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
