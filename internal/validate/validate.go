// Package validate is the validate domain: it reconstructs the environment each
// app would boot with (its non-secret config plus its resolved scan) and
// loads the app's Config against it to check it parses.
package validate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/harrisoncramer/ultra/internal/appcheck"
	"github.com/harrisoncramer/ultra/internal/project"
	"github.com/harrisoncramer/ultra/internal/resolve"
	"github.com/harrisoncramer/ultra/internal/scan"

	"golang.org/x/sync/errgroup"
)

// maxConcurrentApps bounds how many apps are validated at once, so a project
// with many apps doesn't spawn an unbounded number of resolver subprocesses and
// `go run` builds simultaneously.
const maxConcurrentApps = 8

// scanner reports an app's config package import path, which validate needs to
// generate the throwaway program that loads its Config.
type scanner interface {
	ConfigImportPath(dir string) (string, error)
}

// Validator checks that each app's Config parses against its reconstructed
// environment.
type Validator struct {
	checker     *appcheck.Checker
	scanner     scanner
	project     project.Project
	environment string
}

// NewValidatorParams are the dependencies and options NewValidator needs.
type NewValidatorParams struct {
	Scanner            fullScanner
	Project            project.Project
	Environment        string
	RejectUnreferenced bool
	SecretResolver     func(app string) resolve.SecretResolver
	ConfigResolver     resolve.ConfigResolver
}

// fullScanner is the scanner the caller supplies: it reports both an app's
// declared fields (used by the shared checker) and its config import path (used
// by validate to build the loader program).
type fullScanner interface {
	Fields(dir string) ([]scan.Field, error)
	ConfigImportPath(dir string) (string, error)
}

// NewValidator builds a Validator.
func NewValidator(params NewValidatorParams) *Validator {
	return &Validator{
		checker: appcheck.NewChecker(appcheck.NewCheckerParams{
			Scanner:            params.Scanner,
			Project:            params.Project,
			RejectUnreferenced: params.RejectUnreferenced,
			SecretResolver:     params.SecretResolver,
			ConfigResolver:     params.ConfigResolver,
		}),
		scanner:     params.Scanner,
		project:     params.Project,
		environment: params.Environment,
	}
}

// Validate reconstructs each app's boot environment and loads its Config with
// ultra.Load, so caarlos0/env does the checking. It reports each app and returns
// an error if any fail.
func (v *Validator) Validate(ctx context.Context, apps []string) error {
	// Each app is validated independently, so run them concurrently. Validate
	// reports every app even when some fail, so goroutines never return an error
	// (that would cancel the group); each app's outcome lands in its own slot and
	// is reported in input order after the group finishes.
	errs := make([]error, len(apps))
	g := new(errgroup.Group)
	g.SetLimit(maxConcurrentApps)
	for i, appPath := range apps {
		g.Go(func() error {
			errs[i] = v.validateApp(ctx, appPath)
			return nil
		})
	}
	_ = g.Wait()

	failed := 0
	for i, appPath := range apps {
		app := v.project.AppName(appPath)
		if errs[i] != nil {
			failed++
			fmt.Fprintf(os.Stderr, "FAIL  %s: %v\n", app, errs[i])
		} else {
			fmt.Fprintf(os.Stderr, "ok    %s\n", app)
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d app(s) failed validation", failed)
	}
	return nil
}

// validateApp reconstructs one app's boot environment, optionally rejects
// unreferenced keys, then builds and runs a tiny program that loads its Config.
func (v *Validator) validateApp(ctx context.Context, appPath string) error {
	dir := v.project.AppConfigDir(appPath)
	importPath, err := v.scanner.ConfigImportPath(dir)
	if err != nil {
		return err
	}

	// Run the shared static check first. validate is a superset of lint: it fails
	// on the same findings (missing required keys, hardcoded secrets, unreferenced
	// keys), then goes on to do the extra work lint never does, actually loading
	// the app's Config against its reconstructed environment.
	r, err := v.checker.Check(ctx, appPath)
	if err != nil {
		return err
	}
	if !r.Findings.OK() {
		return fmt.Errorf("%s", strings.Join(r.Findings.Problems(), "; "))
	}

	// The full env the app would see: process env, then the platform's non-secret
	// config, then the resolved secrets (real names); later writes win.
	env := os.Environ()
	for k, val := range r.ConfigVals {
		env = append(env, k+"="+val)
	}
	for k, val := range r.SecretVals {
		env = append(env, k+"="+val)
	}

	// The generated program lives inside the app's module so it can import the
	// config package; it is removed once validation finishes. Refuse to touch a
	// pre-existing path so the cleanup below only ever deletes a directory ultra
	// itself created, never a user's.
	genDir := filepath.Join(filepath.Dir(dir), "ultravalidate")
	if _, statErr := os.Stat(genDir); statErr == nil {
		return fmt.Errorf("refusing to generate the validation program: %s already exists; remove or rename it and retry", genDir)
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return fmt.Errorf("checking validation dir %s: %w", genDir, statErr)
	}
	if err := writeValidateMain(genDir, importPath, v.environment); err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(genDir) }()

	// Build and run as two steps with different environments. The build must use
	// the process environment only: the reconstructed env carries the app's own
	// config values, and one named like a go toolchain variable (GOFLAGS, GOCACHE,
	// HOME, ...) would otherwise corrupt the build and produce a failure unrelated
	// to the app's Config. The built program then runs against the reconstructed
	// env, which is what the app's Config parses.
	// bin must be absolute: build runs with its working dir set to genDir, so a
	// relative -o would be written under genDir/genDir and the exec below, run
	// from a different working dir, wouldn't find it. This bit only with a
	// relative --root, where genDir itself is relative.
	bin := filepath.Join(genDir, "validate-bin")
	if abs, err := filepath.Abs(bin); err == nil {
		bin = abs
	}
	build := exec.CommandContext(ctx, "go", "build", "-o", bin, ".")
	build.Dir = genDir
	build.Env = os.Environ()
	var buildErr bytes.Buffer
	build.Stderr = &buildErr
	if err := build.Run(); err != nil {
		if msg := strings.TrimSpace(buildErr.String()); msg != "" {
			return fmt.Errorf("building validation program: %s", msg)
		}
		return fmt.Errorf("building validation program: %w", err)
	}

	cmd := exec.CommandContext(ctx, bin)
	cmd.Env = env
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("%s", redactSecrets(msg, r.SecretVals))
		}
		return err
	}
	return nil
}

// redactSecrets replaces any secret value appearing in s with [redacted], so an
// ultra.Load error that echoes a malformed value (e.g. a non-numeric int) can't
// leak it to the console. Values are masked longest-first so one value contained
// in another is still fully replaced.
func redactSecrets(s string, secretVals map[string]string) string {
	vals := make([]string, 0, len(secretVals))
	for _, v := range secretVals {
		if v != "" {
			vals = append(vals, v)
		}
	}
	sort.Slice(vals, func(i, j int) bool { return len(vals[i]) > len(vals[j]) })
	for _, v := range vals {
		s = strings.ReplaceAll(s, v, "[redacted]")
	}
	return s
}

// writeValidateMain writes a throwaway main.go into dir that imports the app's
// config package and calls ultra.Load, so the app's own module type-checks and
// runs it.
func writeValidateMain(dir, importPath, environment string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating validation dir: %w", err)
	}
	// With an environment, load for it so a field's required tag is enforced only
	// where it applies; without one, load plainly.
	loadCall := "ultra.Load(&config.Config{})"
	if environment != "" {
		loadCall = fmt.Sprintf("ultra.Load(&config.Config{}, ultra.WithEnvironment(%q))", environment)
	}
	src := fmt.Sprintf(`package main

import (
	"fmt"
	"os"

	config %q
	"github.com/harrisoncramer/ultra"
)

func main() {
	if _, err := %s; err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
`, importPath, loadCall)
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o644); err != nil {
		return fmt.Errorf("writing validation main: %w", err)
	}
	return nil
}
