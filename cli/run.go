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
	apps        []string
	configDir   string
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
func prepare(ctx context.Context, p runParams) (*prepared, error) {
	root := p.root
	env := os.Environ()
	composeFiles := []string{filepath.Join(root, "docker-compose.yml")}

	for _, appPath := range p.apps {
		app := appName(appPath)
		names, err := secrets.SecretNames(appConfigDir(root, appPath, p.configDir))
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

		values, err := p.resolverFor(app).Resolve(ctx, names)
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
	prep, err := prepare(ctx, p)
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

// appName is the short name used to namespace an app's secrets, derived from the
// last element of its path — apps/server becomes "server".
func appName(appPath string) string {
	return filepath.Base(appPath)
}

// appConfigDir is the app's config package directory: <appPath>/<configDir>,
// resolved under root unless appPath is already absolute. configDir defaults to
// "config" but can be a nested path like "pkg/config".
func appConfigDir(root, appPath, configDir string) string {
	if configDir == "" {
		configDir = "config"
	}
	if filepath.IsAbs(appPath) {
		return filepath.Join(appPath, configDir)
	}
	return filepath.Join(root, appPath, configDir)
}
