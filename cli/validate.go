package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/harrisoncramer/ultra/pkg/secrets"
)

type validateParams struct {
	root           string
	appsDir        string
	secretResolver func(app string) SecretResolver
	configResolver ConfigResolver
}

// validate reconstructs the environment each app would boot with — its
// non-secret config from the config resolver plus its secrets from the secret
// resolver — and runs the app's own config.Load against it, so caarlos0/env does
// the checking. It reports each app and exits non-zero if any fail.
func validate(ctx context.Context, p validateParams) error {
	apps, err := discoverApps(p.root, p.appsDir)
	if err != nil {
		return err
	}

	failed := 0
	for _, app := range apps {
		if err := validateApp(ctx, p, app); err != nil {
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

func validateApp(ctx context.Context, p validateParams, app string) error {
	dir := configDir(p.root, p.appsDir, app)
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
	if len(names) > 0 {
		secretVals, err := p.secretResolver(app).Resolve(ctx, names)
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
	if err := writeValidateMain(genDir, importPath); err != nil {
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
			return fmt.Errorf("%s", msg)
		}
		return err
	}
	return nil
}

func writeValidateMain(dir, importPath string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	src := fmt.Sprintf(`package main

import (
	"fmt"
	"os"

	config %q
)

func main() {
	if _, err := config.Load(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
`, importPath)
	return os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o644)
}
