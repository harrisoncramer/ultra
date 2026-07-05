// Package runner drives the shared flow every resolver subcommand uses: discover
// apps, generate compose overrides, resolve each app's secrets, and exec the
// given command with them in the environment. Resolver subcommands differ only
// in how they build a resolver, which they pass in via Params.ResolverFor.
package runner

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

// Params configures a run. ResolverFor builds the resolver for a given app, so
// each resolver subcommand supplies its own backend while sharing this flow.
type Params struct {
	Root        string
	AppsDir     string
	ResolverFor func(app string) Resolver
	Command     []string
}

// Run resolves every app's secrets and execs Command with them set in the
// environment and COMPOSE_FILE pointed at the generated overrides. No secret is
// written to disk.
func Run(ctx context.Context, p Params) error {
	apps, err := discoverApps(p.Root, p.AppsDir)
	if err != nil {
		return err
	}

	env := os.Environ()
	composeFiles := []string{filepath.Join(p.Root, "docker-compose.yml")}

	for _, app := range apps {
		names, err := secrets.SecretNames(configDir(p.Root, p.AppsDir, app))
		if err != nil {
			return fmt.Errorf("reading %s config: %w", app, err)
		}
		if len(names) == 0 {
			continue
		}

		override := filepath.Join(p.Root, "tmp", app+".compose.yml")
		if err := os.MkdirAll(filepath.Dir(override), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(override, []byte(compose.ComposeOverride(app, names)), 0o644); err != nil {
			return err
		}
		composeFiles = append(composeFiles, override)

		// One round-trip per app. A missing vault/item is fatal here; a missing
		// individual secret is not.
		values, err := p.ResolverFor(app).Resolve(ctx, names)
		if err != nil {
			return err
		}
		// Inject each secret under an app-namespaced key so two apps sharing a
		// name (e.g. DATABASE_URL) never collide in this shared environment. The
		// override maps the real name back per container via interpolation.
		for name, val := range values {
			env = append(env, compose.ComposeVar(app, name)+"="+val)
		}
		fmt.Fprintf(os.Stderr, "ultra: resolved %d/%d secrets for %s\n", len(values), len(names), app)
	}

	env = append(env, "COMPOSE_FILE="+strings.Join(composeFiles, string(os.PathListSeparator)))

	bin, err := exec.LookPath(p.Command[0])
	if err != nil {
		return fmt.Errorf("command not found: %s", p.Command[0])
	}
	c := exec.CommandContext(ctx, bin, p.Command[1:]...)
	c.Env = env
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
