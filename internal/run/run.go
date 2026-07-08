// Package run is the run domain: it resolves each app's secrets, writes the
// names-only compose overrides that forward them, and execs a command with the
// launcher environment and COMPOSE_FILE set.
package run

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/harrisoncramer/ultra/internal/project"
	"github.com/harrisoncramer/ultra/internal/resolve"
)

// Scanner reports the secret env-var names an app's Config declares.
type Scanner interface {
	SecretNames(dir string) ([]string, error)
}

// Composer renders the compose override and the namespaced launcher variables.
type Composer interface {
	Var(app, name string) string
	Override(app string, names []string) string
}

// Runner resolves apps' secrets and launches commands against them.
type Runner struct {
	scanner     Scanner
	composer    Composer
	project     project.Project
	overrideDir string
	composeFile string
}

// NewRunnerParams are the dependencies and layout NewRunner needs.
type NewRunnerParams struct {
	Scanner     Scanner
	Composer    Composer
	Project     project.Project
	OverrideDir string // dir under root the overrides are written to; empty means "tmp"
	ComposeFile string // base compose file COMPOSE_FILE points at; empty means "docker-compose.yml"
}

// NewRunner builds a Runner, defaulting the override dir and compose file.
func NewRunner(params NewRunnerParams) *Runner {
	overrideDir := params.OverrideDir
	if overrideDir == "" {
		overrideDir = "tmp"
	}
	composeFile := params.ComposeFile
	if composeFile == "" {
		composeFile = "docker-compose.yml"
	}
	return &Runner{
		scanner:     params.Scanner,
		composer:    params.Composer,
		project:     params.Project,
		overrideDir: overrideDir,
		composeFile: composeFile,
	}
}

// Prepared is the result of resolving every app's secrets: the launcher
// environment (with app-namespaced secrets) and the compose files that forward
// them into containers.
type Prepared struct {
	Env          []string
	ComposeFiles []string
}

// Params identify the apps to operate on and how to resolve their secrets.
type Params struct {
	Apps        []string
	ResolverFor func(app string) resolve.SecretResolver
	Command     []string
}

// Prepare discovers every app, resolves its secrets via ResolverFor, writes the
// names-only compose override that forwards them, and returns the launcher env
// (os environment plus app-namespaced secrets) and the compose file list with
// COMPOSE_FILE already appended to env. A store-level failure is fatal; a missing
// individual secret is not. No secret is written to disk.
func (r *Runner) Prepare(ctx context.Context, params Params) (*Prepared, error) {
	env := os.Environ()
	composeFiles := []string{filepath.Join(r.project.Root, r.composeFile)}

	for _, appPath := range params.Apps {
		app := r.project.AppName(appPath)
		names, err := r.scanner.SecretNames(r.project.AppConfigDir(appPath))
		if err != nil {
			return nil, fmt.Errorf("reading %s config: %w", app, err)
		}
		if len(names) == 0 {
			continue
		}

		values, err := params.ResolverFor(app).Resolve(ctx, names)
		if err != nil {
			return nil, err
		}
		// Forward only the secrets the resolver actually returned, and namespace
		// each so two apps sharing a name (e.g. DATABASE_URL) don't collide in this
		// shared env; the override maps it back per service. A name the store lacks
		// is left out of the override entirely, so the value the platform already
		// provides (e.g. the compose environment) survives instead of being
		// overridden with an empty one.
		resolved := make([]string, 0, len(values))
		for name, val := range values {
			resolved = append(resolved, name)
			env = append(env, r.composer.Var(app, name)+"="+val)
		}
		sort.Strings(resolved)

		if len(resolved) > 0 {
			override := filepath.Join(r.project.Root, r.overrideDir, app+".compose.yml")
			if err := os.MkdirAll(filepath.Dir(override), 0o755); err != nil {
				return nil, fmt.Errorf("creating override dir for %s: %w", app, err)
			}
			if err := os.WriteFile(override, []byte(r.composer.Override(app, resolved)), 0o644); err != nil {
				return nil, fmt.Errorf("writing compose override for %s: %w", app, err)
			}
			composeFiles = append(composeFiles, override)
		}
		fmt.Fprintf(os.Stderr, "ultra: resolved %d/%d secrets for %s\n", len(values), len(names), app)
	}

	env = append(env, "COMPOSE_FILE="+strings.Join(composeFiles, string(os.PathListSeparator)))
	return &Prepared{Env: env, ComposeFiles: composeFiles}, nil
}

// Run resolves every app's secrets and execs the command with them set in the
// environment and COMPOSE_FILE pointed at the generated overrides.
func (r *Runner) Run(ctx context.Context, params Params) error {
	prep, err := r.Prepare(ctx, params)
	if err != nil {
		return err
	}

	bin, err := exec.LookPath(params.Command[0])
	if err != nil {
		return fmt.Errorf("command not found: %s", params.Command[0])
	}
	c := exec.CommandContext(ctx, bin, params.Command[1:]...)
	c.Env = prep.Env
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}
