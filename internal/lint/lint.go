// Package lint is the lint domain: it statically checks that every required key
// an app declares will be provided, without constructing or parsing any value,
// so it runs where the real secret values aren't reachable.
package lint

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/harrisoncramer/ultra/internal/project"
	"github.com/harrisoncramer/ultra/internal/resolve"
	"github.com/harrisoncramer/ultra/internal/scan"
)

// Scanner reports an app's declared fields.
type Scanner interface {
	Fields(dir string) ([]scan.Field, error)
}

// Linter checks each app's required keys against what its resolvers provide.
type Linter struct {
	scanner            Scanner
	project            project.Project
	environment        string
	rejectUnreferenced bool
	secretResolver     func(app string) resolve.SecretResolver
	configResolver     resolve.ConfigResolver
}

// NewLinterParams are the dependencies and options NewLinter needs.
type NewLinterParams struct {
	Scanner            Scanner
	Project            project.Project
	Environment        string
	RejectUnreferenced bool
	SecretResolver     func(app string) resolve.SecretResolver
	ConfigResolver     resolve.ConfigResolver
}

// NewLinter builds a Linter.
func NewLinter(params NewLinterParams) *Linter {
	return &Linter{
		scanner:            params.Scanner,
		project:            params.Project,
		environment:        params.Environment,
		rejectUnreferenced: params.RejectUnreferenced,
		secretResolver:     params.SecretResolver,
		configResolver:     params.ConfigResolver,
	}
}

// findings is what checkApp reports for one app: the required keys no resolver
// provides, and — when rejectUnreferenced is set — the keys a resolver provides
// that no Config field references.
type findings struct {
	missing []string
	extra   []string
}

// Lint checks that every required key each app declares will be provided, and
// reports each app. It returns an error if any app is missing a required key or
// (with rejectUnreferenced) is handed an unreferenced one.
func (l *Linter) Lint(ctx context.Context, apps []string) error {
	failed := 0
	for _, appPath := range apps {
		app := l.project.AppName(appPath)
		found, err := l.checkApp(ctx, appPath)
		switch {
		case err != nil:
			failed++
			fmt.Fprintf(os.Stderr, "FAIL  %s: %v\n", app, err)
		case len(found.missing) > 0 || len(found.extra) > 0:
			failed++
			var problems []string
			if len(found.missing) > 0 {
				problems = append(problems, "missing required keys: "+strings.Join(found.missing, ", "))
			}
			if len(found.extra) > 0 {
				problems = append(problems, "unreferenced keys provided: "+strings.Join(found.extra, ", "))
			}
			fmt.Fprintf(os.Stderr, "FAIL  %s: %s\n", app, strings.Join(problems, "; "))
		default:
			fmt.Fprintf(os.Stderr, "ok    %s\n", app)
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d app(s) failed lint", failed)
	}
	return nil
}

// checkApp reports the required keys app declares that neither resolver provides,
// and — when rejectUnreferenced is set — the keys a resolver provides that no
// Config field references. Secret fields are checked against the secret
// resolver's keys and non-secret fields against the config resolver's; the
// resolved values themselves are ignored, only the presence of each key matters.
func (l *Linter) checkApp(ctx context.Context, appPath string) (findings, error) {
	app := l.project.AppName(appPath)
	fields, err := l.scanner.Fields(l.project.AppConfigDir(appPath))
	if err != nil {
		return findings{}, err
	}

	var secretNames []string
	for _, f := range fields {
		if f.Secret {
			secretNames = append(secretNames, f.Name)
		}
	}

	secretVals := map[string]string{}
	if len(secretNames) > 0 {
		secretVals, err = l.secretResolver(app).Resolve(ctx, secretNames)
		if err != nil {
			return findings{}, err
		}
	}

	configVals, err := l.configResolver.Resolve(ctx, app)
	if err != nil {
		return findings{}, err
	}

	var missing []string
	for _, f := range fields {
		if !f.RequiredIn(l.environment) {
			continue
		}
		provided := configVals
		if f.Secret {
			provided = secretVals
		}
		if _, ok := provided[f.Name]; !ok {
			missing = append(missing, f.Name)
		}
	}
	sort.Strings(missing)

	var extra []string
	if l.rejectUnreferenced {
		declared := scan.DeclaredNames(fields)
		extra = append(scan.Unreferenced(secretVals, declared), scan.Unreferenced(configVals, declared)...)
		sort.Strings(extra)
	}
	return findings{missing: missing, extra: extra}, nil
}
