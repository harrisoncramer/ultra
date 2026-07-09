// Package run is the run domain: it reads each app's declared secrets through
// the shared configreader, resolves their values from the secret store, and
// execs a command with the launcher environment and COMPOSE_FILE set. It never
// writes a compose file; it points COMPOSE_FILE at the override gen already
// wrote, so generation and running stay separate concerns.
package run

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/harrisoncramer/ultra/internal/configreader"
	"github.com/harrisoncramer/ultra/internal/project"
	"github.com/harrisoncramer/ultra/internal/resolve"
	pkgcompose "github.com/harrisoncramer/ultra/pkg/compose"

	"golang.org/x/sync/errgroup"
)

// maxConcurrentApps bounds how many apps resolve at once, so a project with many
// apps doesn't spawn an unbounded number of resolver subprocesses (op, docker)
// simultaneously.
const maxConcurrentApps = 8

// configReader reports the secret names each app's Config declares. It is the
// shared source of truth gen and run both read, so run resolves exactly the
// secrets gen wrote into the committed override.
type configReader interface {
	Read(apps []string) ([]configreader.AppOutput, error)
}

// composer renders the namespaced launcher variable a secret is passed through.
type composer interface {
	Var(app, name string) string
}

// Runner reads apps' declared secrets, resolves them, and launches commands with
// them injected. It does not generate the compose override; gen does.
type Runner struct {
	reader       configReader
	composer     composer
	project      project.Project
	composeFile  string
	overridePath string
}

// NewRunnerParams are the dependencies and layout NewRunner needs.
type NewRunnerParams struct {
	Reader       configReader
	Composer     composer
	Project      project.Project
	ComposeFile  string // base compose file COMPOSE_FILE points at; empty means "docker-compose.yml"
	OverridePath string // path to the committed override gen wrote; required when any app declares a secret
}

// NewRunner builds a Runner, defaulting the compose file.
func NewRunner(params NewRunnerParams) *Runner {
	composeFile := params.ComposeFile
	if composeFile == "" {
		composeFile = "docker-compose.yml"
	}
	return &Runner{
		reader:       params.Reader,
		composer:     params.Composer,
		project:      params.Project,
		composeFile:  composeFile,
		overridePath: params.OverridePath,
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

// prepare reads every app's declared secrets, resolves each app's secrets via
// ResolverFor, and returns the launcher env (os environment plus app-namespaced
// secrets) and the compose file list with COMPOSE_FILE already appended to env.
// A store-level failure is fatal; a missing individual secret is not. When any
// app declares a secret, the committed override gen wrote must exist, since it
// is what forwards the secrets into containers. Nothing is written to disk.
func (r *Runner) prepare(ctx context.Context, params Params) (*prepared, error) {
	// Read the declared secret names from config, the shared source of truth, so
	// run resolves exactly what gen wrote into the override. run never generates
	// the override itself.
	overrides, err := r.reader.Read(params.Apps)
	if err != nil {
		return nil, err
	}

	// Verify the committed override before touching the secret store, so a stale
	// or missing file fails fast with no store round-trips.
	composeFiles, err := r.overrideComposeFiles(overrides)
	if err != nil {
		return nil, err
	}

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

	for i := range overrides {
		env = append(env, results[i].envVars...)
		if results[i].log != "" {
			fmt.Fprint(os.Stderr, results[i].log)
		}
	}

	env = append(env, "COMPOSE_FILE="+strings.Join(composeFiles, string(os.PathListSeparator)))
	return &prepared{env: env, composeFiles: composeFiles}, nil
}

// overrideComposeFiles returns the base compose file plus, when any app declares
// a secret, the committed override gen wrote. It requires that override to exist
// and still match the current Config: a declared secret whose fingerprint the
// override doesn't carry would be resolved into the env but never forwarded, so
// run fails loudly rather than dropping it silently. It never contacts the store.
func (r *Runner) overrideComposeFiles(overrides []configreader.AppOutput) ([]string, error) {
	files := []string{filepath.Join(r.project.Root, r.composeFile)}
	if !declaresSecret(overrides) {
		return files, nil
	}
	if r.overridePath == "" {
		return nil, fmt.Errorf("no override path configured; run `ultra gen` first")
	}
	data, err := os.ReadFile(r.overridePath)
	if err != nil {
		return nil, fmt.Errorf("secret override %s not found; run `ultra gen` first: %w", r.overridePath, err)
	}
	fingerprints := pkgcompose.ParseFingerprints(string(data))
	var stale []string
	for _, o := range overrides {
		if len(o.Names) == 0 {
			continue
		}
		if fingerprints[o.App] != pkgcompose.Fingerprint(o.Names) {
			stale = append(stale, o.App)
		}
	}
	if len(stale) > 0 {
		return nil, fmt.Errorf("secret override %s is stale for %s; run `ultra gen`", r.overridePath, strings.Join(stale, ", "))
	}
	return append(files, r.overridePath), nil
}

// declaresSecret reports whether any app declares at least one secret.
func declaresSecret(overrides []configreader.AppOutput) bool {
	for _, o := range overrides {
		if len(o.Names) > 0 {
			return true
		}
	}
	return false
}

// Run resolves every app's secrets and execs the command with them set in the
// environment and COMPOSE_FILE pointed at the committed override gen wrote.
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
