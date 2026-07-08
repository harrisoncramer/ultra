// Package run is the run domain: it generates the combined names-only compose
// override, resolves each app's secrets, and execs a command with the launcher
// environment and COMPOSE_FILE set.
package run

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/harrisoncramer/ultra/internal/gen"
	"github.com/harrisoncramer/ultra/internal/project"
	"github.com/harrisoncramer/ultra/internal/resolve"

	"golang.org/x/sync/errgroup"
)

// maxConcurrentApps bounds how many apps resolve at once, so a project with many
// apps doesn't spawn an unbounded number of resolver subprocesses (op, docker)
// simultaneously.
const maxConcurrentApps = 8

// generator writes the combined compose override and reports the secret names
// each app declares, independent of the secret store.
type generator interface {
	Generate(apps []string) (gen.Result, error)
}

// composer renders the namespaced launcher variable a secret is passed through.
type composer interface {
	Var(app, name string) string
}

// Runner generates apps' overrides, resolves their secrets, and launches
// commands against them.
type Runner struct {
	generator   generator
	composer    composer
	project     project.Project
	composeFile string
}

// NewRunnerParams are the dependencies and layout NewRunner needs.
type NewRunnerParams struct {
	Generator   generator
	Composer    composer
	Project     project.Project
	ComposeFile string // base compose file COMPOSE_FILE points at; empty means "docker-compose.yml"
}

// NewRunner builds a Runner, defaulting the compose file.
func NewRunner(params NewRunnerParams) *Runner {
	composeFile := params.ComposeFile
	if composeFile == "" {
		composeFile = "docker-compose.yml"
	}
	return &Runner{
		generator:   params.Generator,
		composer:    params.Composer,
		project:     params.Project,
		composeFile: composeFile,
	}
}

// prepared is the result of resolving every app's secrets: the launcher
// environment (with app-namespaced secrets) and the compose files that forward
// them into containers.
type prepared struct {
	env          []string
	composeFiles []string
}

// Params identify the apps to operate on and how to resolve their secrets.
type Params struct {
	Apps        []string
	ResolverFor func(app string) resolve.SecretResolver
	Command     []string
}

// prepare generates every app's compose override, resolves each app's secrets
// via ResolverFor, and returns the launcher env (os environment plus
// app-namespaced secrets) and the compose file list with COMPOSE_FILE already
// appended to env. A store-level failure is fatal; a missing individual secret
// is not. No secret is written to disk.
func (r *Runner) prepare(ctx context.Context, params Params) (*prepared, error) {
	// The override is static (it lists the declared secret names, not values),
	// so generate it up front without the store; run only resolves and injects.
	result, err := r.generator.Generate(params.Apps)
	if err != nil {
		return nil, err
	}
	overrides := result.Apps

	// Each app's secret store round-trip is independent, so fire them all off
	// concurrently and let the errgroup cancel the rest on the first store-level
	// failure. Results land in per-app slots so the merged env stays in the apps'
	// input order.
	type appResult struct {
		envVars []string
		log     string
	}
	results := make([]appResult, len(overrides))

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrentApps)
	for i, o := range overrides {
		if len(o.Names) == 0 {
			continue
		}
		g.Go(func() error {
			values, err := params.ResolverFor(o.App).Resolve(ctx, o.Names)
			if err != nil {
				return err
			}
			// Forward only the secrets the resolver actually returned, and namespace
			// each so two apps sharing a name (e.g. DATABASE_URL) don't collide in this
			// shared env; the override maps it back per service. A name the store lacks
			// is left unset, so its override entry interpolates to empty rather than
			// carrying another app's value.
			envVars := make([]string, 0, len(values))
			for name, val := range values {
				envVars = append(envVars, r.composer.Var(o.App, name)+"="+val)
			}
			results[i] = appResult{
				envVars: envVars,
				log:     fmt.Sprintf("ultra: resolved %d/%d secrets for %s\n", len(values), len(o.Names), o.App),
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}

	env := os.Environ()
	composeFiles := []string{filepath.Join(r.project.Root, r.composeFile)}
	if result.Path != "" {
		composeFiles = append(composeFiles, result.Path)
	}
	for i := range overrides {
		env = append(env, results[i].envVars...)
		if results[i].log != "" {
			fmt.Fprint(os.Stderr, results[i].log)
		}
	}

	env = append(env, "COMPOSE_FILE="+strings.Join(composeFiles, string(os.PathListSeparator)))
	return &prepared{env: env, composeFiles: composeFiles}, nil
}

// Run resolves every app's secrets and execs the command with them set in the
// environment and COMPOSE_FILE pointed at the generated overrides.
func (r *Runner) Run(ctx context.Context, params Params) error {
	prep, err := r.prepare(ctx, params)
	if err != nil {
		return err
	}

	bin, err := exec.LookPath(params.Command[0])
	if err != nil {
		return fmt.Errorf("command not found: %s", params.Command[0])
	}
	c := exec.CommandContext(ctx, bin, params.Command[1:]...)
	c.Env = prep.env
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}
