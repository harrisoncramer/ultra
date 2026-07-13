// Package appcheck holds the per-app static check shared by the lint and validate
// domains. It scans an app's declared fields, resolves its secret and non-secret
// config, and reports the required keys no resolver provides, the secrets
// hardcoded in the non-secret config, and the unreferenced keys a resolver hands
// back. lint reports these findings; validate fails on the same findings and then
// does the extra work of actually loading the app's Config. validate is thus a
// superset of lint's static pass, and the two can never diverge on what counts as
// a problem.
package appcheck

import (
	"context"
	"sort"
	"strings"

	"github.com/harrisoncramer/ultra/internal/project"
	"github.com/harrisoncramer/ultra/internal/resolve"
	"github.com/harrisoncramer/ultra/internal/scan"
)

// scanner reports an app's declared fields.
type scanner interface {
	Fields(dir string) ([]scan.Field, error)
}

// Checker resolves an app's declared secrets and non-secret config and runs the
// static checks common to lint and validate.
type Checker struct {
	scanner            scanner
	project            project.Project
	environment        string
	rejectUnreferenced bool
	secretResolver     func(app string) resolve.SecretResolver
	configResolver     resolve.ConfigResolver
}

// NewCheckerParams are the dependencies and options NewChecker needs.
type NewCheckerParams struct {
	Scanner            scanner
	Project            project.Project
	Environment        string
	RejectUnreferenced bool
	SecretResolver     func(app string) resolve.SecretResolver
	ConfigResolver     resolve.ConfigResolver
}

// NewChecker builds a Checker.
func NewChecker(params NewCheckerParams) *Checker {
	return &Checker{
		scanner:            params.Scanner,
		project:            params.Project,
		environment:        params.Environment,
		rejectUnreferenced: params.RejectUnreferenced,
		secretResolver:     params.SecretResolver,
		configResolver:     params.ConfigResolver,
	}
}

// Findings is the static problems one app has. Missing holds the required keys no
// resolver provides; Leaked holds the secrets whose value is hardcoded in the
// non-secret config; Extra holds the keys a resolver provides that no Config field
// references, and is populated only when rejectUnreferenced is set.
type Findings struct {
	Missing []string
	Leaked  []string
	Extra   []string
}

// OK reports whether the app has no static problems.
func (f Findings) OK() bool {
	return len(f.Missing) == 0 && len(f.Leaked) == 0 && len(f.Extra) == 0
}

// Problems renders each non-empty finding as a human-readable clause, so lint and
// validate word the same problem the same way.
func (f Findings) Problems() []string {
	var problems []string
	if len(f.Missing) > 0 {
		problems = append(problems, "missing required keys: "+strings.Join(f.Missing, ", "))
	}
	if len(f.Leaked) > 0 {
		problems = append(problems, "secrets hardcoded in non-secret config: "+strings.Join(f.Leaked, ", "))
	}
	if len(f.Extra) > 0 {
		problems = append(problems, "unreferenced keys provided: "+strings.Join(f.Extra, ", "))
	}
	return problems
}

// Result is the outcome of checking one app: its resolved secret and non-secret
// values, and the static findings. The resolved values are returned so callers
// (validate) can reuse them to reconstruct the app's environment without
// resolving again.
type Result struct {
	SecretVals map[string]string
	ConfigVals map[string]string
	Findings   Findings
}

// Check scans the app's Config, resolves its secret fields against the secret
// resolver and its non-secret fields against the config resolver, and reports the
// missing required keys, hardcoded-secret leaks, and (when enabled) unreferenced
// provided keys. Secret fields are checked against the secret resolver's keys and
// non-secret fields against the config resolver's; only the presence of each key
// matters, never its value.
func (c *Checker) Check(ctx context.Context, appPath string) (Result, error) {
	app := c.project.AppName(appPath)

	fields, err := c.scanner.Fields(c.project.AppConfigDir(appPath))
	if err != nil {
		return Result{}, err
	}

	var secretNames []string
	for _, f := range fields {
		if f.IsSecret {
			secretNames = append(secretNames, f.Name)
		}
	}

	// Only hit the secret store if the app actually declares a secret; an app with
	// none has no store item to fetch.
	secretVals := map[string]string{}
	if len(secretNames) > 0 {
		secretVals, err = c.secretResolver(app).Resolve(ctx, secretNames)
		if err != nil {
			return Result{}, err
		}
	}

	configVals, err := c.configResolver.Resolve(ctx, app)
	if err != nil {
		return Result{}, err
	}

	// A required key must be handed back by one of the resolvers: secret fields by
	// the secret resolver, non-secret fields by the config resolver.
	var missing []string
	for _, f := range fields {
		if !f.RequiredIn(c.environment) {
			continue
		}
		provided := configVals
		if f.IsSecret {
			provided = secretVals
		}
		if _, ok := provided[f.Name]; !ok {
			missing = append(missing, f.Name)
		}
	}
	sort.Strings(missing)

	// A secret whose value lives in the non-secret config is a leak: secrets are
	// meant to come from the store, not committed config. Only resolvers that can
	// tell a literal from a forwarded reference (docker-compose) report these.
	var leaked []string
	if lc, ok := c.configResolver.(resolve.SecretLeakChecker); ok && len(secretNames) > 0 {
		leaked, err = lc.LeakedSecrets(ctx, app, secretNames)
		if err != nil {
			return Result{}, err
		}
		sort.Strings(leaked)
	}

	// With rejectUnreferenced, a resolver handing back a key no Config field reads
	// is config drift, a stale compose var or a store entry nothing consumes, so
	// surface it rather than silently ignoring it.
	var extra []string
	if c.rejectUnreferenced {
		declared := scan.DeclaredNames(fields)
		extra = append(scan.Unreferenced(secretVals, declared), scan.Unreferenced(configVals, declared)...)
		sort.Strings(extra)
	}

	return Result{
		SecretVals: secretVals,
		ConfigVals: configVals,
		Findings: Findings{
			Missing: missing,
			Leaked:  leaked,
			Extra:   extra,
		},
	}, nil
}
