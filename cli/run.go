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
	resolverFor func(app string) Resolver
	command     []string
}

// run resolves every app's secrets and execs command with them set in the
// environment and COMPOSE_FILE pointed at the generated overrides. No secret is
// written to disk.
func run(ctx context.Context, p runParams) error {
	apps, err := discoverApps(p.root, p.appsDir)
	if err != nil {
		return err
	}

	env := os.Environ()
	composeFiles := []string{filepath.Join(p.root, "docker-compose.yml")}

	for _, app := range apps {
		names, err := secrets.SecretNames(configDir(p.root, p.appsDir, app))
		if err != nil {
			return fmt.Errorf("reading %s config: %w", app, err)
		}
		if len(names) == 0 {
			continue
		}

		override := filepath.Join(p.root, "tmp", app+".compose.yml")
		if err := os.MkdirAll(filepath.Dir(override), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(override, []byte(compose.ComposeOverride(app, names)), 0o644); err != nil {
			return err
		}
		composeFiles = append(composeFiles, override)

		// One round-trip per app. A store-level failure is fatal here; a missing
		// individual secret is not.
		values, err := p.resolverFor(app).Resolve(ctx, names)
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

	bin, err := exec.LookPath(p.command[0])
	if err != nil {
		return fmt.Errorf("command not found: %s", p.command[0])
	}
	c := exec.CommandContext(ctx, bin, p.command[1:]...)
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
