package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/harrisoncramer/ultra/pkg/secrets"
)

type validateParams struct {
	root           string
	apps           []string
	configDir      string
	environment    string
	secretResolver func(app string) SecretResolver
	configResolver ConfigResolver
}

// validate reconstructs the environment each app would boot with — its
// non-secret config from the config resolver plus its secrets from the secret
// resolver — and loads the app's Config with ultra.Load against it, so
// caarlos0/env does the checking. It reports each app and exits non-zero if any
// fail.
func validate(ctx context.Context, p validateParams) error {
	failed := 0
	for _, appPath := range p.apps {
		app := appName(appPath)
		if err := validateApp(ctx, p, appPath); err != nil {
			failed++
			fmt.Fprintf(os.Stderr, "FAIL  %s: %v\n", app, err)
		} else {
			fmt.Fprintf(os.Stderr, "ok    %s\n", app)
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d app(s) failed validation", failed)
	}
	return nil
}

func validateApp(ctx context.Context, p validateParams, appPath string) error {
	app := appName(appPath)
	dir := appConfigDir(p.root, appPath, p.configDir)
	importPath, err := secrets.ConfigImportPath(dir)
	if err != nil {
		return err
	}
	names, err := secrets.SecretNames(dir)
	if err != nil {
		return err
	}

	// The full env the app would see: process env, then the platform's non-secret
	// config, then the resolved secrets (real names) — later writes win.
	env := os.Environ()

	configVals, err := p.configResolver.Resolve(ctx, app)
	if err != nil {
		return err
	}
	for k, v := range configVals {
		env = append(env, k+"="+v)
	}

	// Only hit the secret store if the app actually declares secrets; an app with
	// none (e.g. no secret-tagged fields) has no vault item to fetch.
	var secretVals map[string]string
	if len(names) > 0 {
		secretVals, err = p.secretResolver(app).Resolve(ctx, names)
		if err != nil {
			return err
		}
		for k, v := range secretVals {
			env = append(env, k+"="+v)
		}
	}

	// The generated program lives inside the app's module so it can import the
	// config package; it is removed once validation finishes.
	genDir := filepath.Join(filepath.Dir(dir), "ultravalidate")
	if err := writeValidateMain(genDir, importPath, p.environment); err != nil {
		return err
	}
	defer os.RemoveAll(genDir)

	cmd := exec.CommandContext(ctx, "go", "run", ".")
	cmd.Dir = genDir
	cmd.Env = env
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("%s", redactSecrets(msg, secretVals))
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

func writeValidateMain(dir, importPath, environment string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// With an environment, load for it so envScope-tagged fields are required only
	// where they apply; without one, load plainly.
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
	return os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o644)
}
