package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	compose "github.com/harrisoncramer/ultra/pkg/compose"
	"github.com/harrisoncramer/ultra/pkg/secrets"
)

type runParams struct {
	root        string
	appsDir     string
	resolverFor func(app string) SecretResolver
	command     []string
}

// prepared is the shared result of resolving every app's secrets: the launcher
// environment (with app-namespaced secrets) and the compose files that forward
// them into containers. Both run and validate build on it.
type prepared struct {
	env          []string
	composeFiles []string
}

// prepare discovers every app, resolves its secrets via resolverFor, writes the
// names-only compose override that forwards them, and returns the launcher env
// (os environment plus app-namespaced secrets) and the compose file list with
// COMPOSE_FILE already appended to env. A store-level failure is fatal; a missing
// individual secret is not. No secret is written to disk.
func prepare(ctx context.Context, root, appsDir string, resolverFor func(app string) SecretResolver) (*prepared, error) {
	apps, err := discoverApps(root, appsDir)
	if err != nil {
		return nil, err
	}

	env := os.Environ()
	composeFiles := []string{filepath.Join(root, "docker-compose.yml")}

	for _, app := range apps {
		names, err := secrets.SecretNames(configDir(root, appsDir, app))
		if err != nil {
			return nil, fmt.Errorf("reading %s config: %w", app, err)
		}
		if len(names) == 0 {
			continue
		}

		override := filepath.Join(root, "tmp", app+".compose.yml")
		if err := os.MkdirAll(filepath.Dir(override), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(override, []byte(compose.ComposeOverride(app, names)), 0o644); err != nil {
			return nil, err
		}
		composeFiles = append(composeFiles, override)

		values, err := resolverFor(app).Resolve(ctx, names)
		if err != nil {
			return nil, err
		}
		// Namespace each secret so two apps sharing a name (e.g. DATABASE_URL)
		// don't collide in this shared env; the override maps it back per service.
		for name, val := range values {
			env = append(env, compose.ComposeVar(app, name)+"="+val)
		}
		fmt.Fprintf(os.Stderr, "ultra: resolved %d/%d secrets for %s\n", len(values), len(names), app)
	}

	env = append(env, "COMPOSE_FILE="+strings.Join(composeFiles, string(os.PathListSeparator)))
	return &prepared{env: env, composeFiles: composeFiles}, nil
}

// run resolves every app's secrets and execs command with them set in the
// environment and COMPOSE_FILE pointed at the generated overrides.
func run(ctx context.Context, p runParams) error {
	prep, err := prepare(ctx, p.root, p.appsDir, p.resolverFor)
	if err != nil {
		return err
	}

	bin, err := exec.LookPath(p.command[0])
	if err != nil {
		return fmt.Errorf("command not found: %s", p.command[0])
	}
	c := exec.CommandContext(ctx, bin, p.command[1:]...)
	c.Env = prep.env
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

// discoverApps returns the names of every directory under <root>/<appsDir> that
// has a config package.
func discoverApps(root, appsDir string) ([]string, error) {
	dir := filepath.Join(root, appsDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("listing apps in %s: %w", dir, err)
	}
	var apps []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if info, err := os.Stat(configDir(root, appsDir, e.Name())); err == nil && info.IsDir() {
			apps = append(apps, e.Name())
		}
	}
	return apps, nil
}

func configDir(root, appsDir, app string) string {
	return filepath.Join(root, appsDir, app, "config")
}
