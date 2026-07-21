// Package run is the run domain: it generates the combined names-only compose
// override into an ephemeral location, resolves each app's secrets, and execs a
// command with the launcher environment and COMPOSE_FILE set. The override is
// regenerated on every run from the current Config, so it is always current and
// never committed.
package run

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/harrisoncramer/ultra/internal/compose"
	"github.com/harrisoncramer/ultra/internal/gen"
	"github.com/harrisoncramer/ultra/internal/project"
	"github.com/harrisoncramer/ultra/internal/resolve"

	"golang.org/x/sync/errgroup"
)

// DefaultConcurrency bounds how many apps resolve at once when the caller does
// not set one, so a project with many apps doesn't spawn an unbounded number of
// resolver subprocesses (op, docker) simultaneously. It comfortably covers a
// typical project's app count so every app resolves in one wave; --concurrency
// tunes it per invocation.
const DefaultConcurrency = 16

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
	generator    generator
	composer     composer
	project      project.Project
	composeFiles []string
	concurrency  int
}

// NewRunnerParams are the dependencies and layout NewRunner needs.
type NewRunnerParams struct {
	Generator generator
	Composer  composer
	Project   project.Project
	// ComposeFiles are the base compose files COMPOSE_FILE points at, in order, so
	// a later file (e.g. a local override) wins over an earlier one; the generated
	// secrets override is always layered last. Empty means "docker-compose.yml".
	ComposeFiles []string
	// Concurrency bounds how many apps resolve their secrets at once. Zero or
	// negative means DefaultConcurrency.
	Concurrency int
}

// NewRunner builds a Runner, defaulting the compose file and concurrency.
func NewRunner(params NewRunnerParams) *Runner {
	composeFiles := params.ComposeFiles
	if len(composeFiles) == 0 {
		composeFiles = []string{"docker-compose.yml"}
	}
	concurrency := params.Concurrency
	if concurrency <= 0 {
		concurrency = DefaultConcurrency
	}
	return &Runner{
		generator:    params.Generator,
		composer:     params.Composer,
		project:      params.Project,
		composeFiles: composeFiles,
		concurrency:  concurrency,
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

// prepare regenerates every app's compose override, resolves each app's secrets
// via ResolverFor, and returns the launcher env (os environment plus
// app-namespaced secrets) and the compose file list with COMPOSE_FILE already
// appended to env. A store-level failure is fatal; a missing individual secret
// is not. No secret value is written to disk; only the names-only override is.
func (r *Runner) prepare(ctx context.Context, params Params) (*prepared, error) {
	// The override's service key is each app's name, which must match a service in
	// the base compose or docker rejects the merged project ("neither an image nor
	// a build context") and no service starts. Drop any app whose name isn't a
	// base service, warning why, so a dir-name/service-name mismatch degrades to a
	// clear message and the rest of the stack still runs, rather than a cryptic
	// docker failure. Only filter when the base compose is readable; run may exec
	// a non-docker command that never consults it.
	apps := r.scopeToComposeServices(params.Apps)

	// The override lists the declared secret names, not values, and is cheap to
	// produce, so regenerate it every run from the current Config. That keeps it
	// in sync with the code without a separate step or a committed file.
	result, err := r.generator.Generate(apps)
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
	g.SetLimit(r.concurrency)
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
	// COMPOSE_FILE lists the base files in order, then the generated secrets
	// override last. docker merges left to right, so a later base file (a local
	// override a developer layers on) wins over an earlier one, while the secrets
	// override still applies on top since it maps distinct launcher variables.
	composeFiles := make([]string, 0, len(r.composeFiles)+1)
	for _, f := range r.composeFiles {
		composeFiles = append(composeFiles, filepath.Join(r.project.Root, f))
	}
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

// scopeToComposeServices drops any app whose name isn't a service in the base
// compose files, warning for each, so its override block can't turn the merged
// project into one docker rejects. Service names are unioned across every base
// file, since a later file may add a service the first doesn't. It returns the
// apps unchanged when no base file can be read (they may be absent, or the
// command may not be docker), so this only ever removes an app that would
// otherwise have broken the run.
func (r *Runner) scopeToComposeServices(apps []string) []string {
	services := map[string]bool{}
	readAny := false
	for _, f := range r.composeFiles {
		names, err := compose.ServiceNames(filepath.Join(r.project.Root, f))
		if err != nil {
			continue
		}
		readAny = true
		for name := range names {
			services[name] = true
		}
	}
	if !readAny {
		return apps
	}
	kept := make([]string, 0, len(apps))
	for _, appPath := range apps {
		name := r.project.AppName(appPath)
		if services[name] {
			kept = append(kept, appPath)
			continue
		}
		fmt.Fprintf(os.Stderr, "ultra: %s has no service %q in the compose files, skipping it; its secrets won't be injected\n", appPath, name)
	}
	return kept
}

// Run resolves every app's secrets and execs the command with them set in the
// environment and COMPOSE_FILE pointed at the regenerated overrides.
func (r *Runner) Run(ctx context.Context, params Params) error {
	// Guard here as well as in the CLI: Run is exported, and a caller passing an
	// empty command would otherwise panic on Command[0] below.
	if len(params.Command) == 0 {
		return fmt.Errorf("no command to run: pass a command to exec")
	}
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
