// Package lint is the lint domain: it statically checks that every required key
// an app declares will be provided, without constructing or parsing any value,
// so it runs where the real secret values aren't reachable.
package lint

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/harrisoncramer/ultra/internal/appcheck"
	"github.com/harrisoncramer/ultra/internal/project"
	"github.com/harrisoncramer/ultra/internal/resolve"
	"github.com/harrisoncramer/ultra/internal/scan"

	"golang.org/x/sync/errgroup"
)

// maxConcurrentApps bounds how many apps are checked at once, so a project with
// many apps doesn't spawn an unbounded number of resolver subprocesses (op,
// docker) simultaneously.
const maxConcurrentApps = 8

// scanner reports an app's declared fields.
type scanner interface {
	// Fields pulls the fields out of a user's Config and reports metadata about them, such
	// as what environments they're required in, and whether they're secret or not.
	Fields(dir string) ([]scan.Field, error)
}

// Linter checks each app's required keys against what its resolvers provide.
type Linter struct {
	checker *appcheck.Checker
	project project.Project
}

// NewLinterParams are the dependencies and options NewLinter needs.
type NewLinterParams struct {
	Scanner            scanner
	Project            project.Project
	Environment        string
	RejectUnreferenced bool
	SecretResolver     func(app string) resolve.SecretResolver
	ConfigResolver     resolve.ConfigResolver
}

// NewLinter builds a Linter.
func NewLinter(params NewLinterParams) *Linter {
	return &Linter{
		checker: appcheck.NewChecker(appcheck.NewCheckerParams{
			Scanner:            params.Scanner,
			Project:            params.Project,
			Environment:        params.Environment,
			RejectUnreferenced: params.RejectUnreferenced,
			SecretResolver:     params.SecretResolver,
			ConfigResolver:     params.ConfigResolver,
		}),
		project: params.Project,
	}
}

// Lint checks that every required key each app declares will be provided, and
// reports each app. It returns an error if any app is missing a required key,
// has a secret hardcoded in its non-secret config, or (with rejectUnreferenced)
// is handed an unreferenced one.
func (l *Linter) Lint(ctx context.Context, apps []string) error {

	// Check every app concurrently; each app's resolver round-trips are
	// independent. Lint reports all apps even when some fail, so goroutines never
	// return an error (that would cancel the group); each app's result lands in
	// its own slot and is reported in input order.
	type result struct {
		found appcheck.Findings
		err   error
	}
	results := make([]result, len(apps))
	g := new(errgroup.Group)
	g.SetLimit(maxConcurrentApps)

	for i, appPath := range apps {
		g.Go(func() error {
			r, err := l.checker.Check(ctx, appPath)
			results[i] = result{found: r.Findings, err: err}
			return nil
		})
	}
	_ = g.Wait()

	failed := 0
	for i, appPath := range apps {
		app := l.project.AppName(appPath)
		found, err := results[i].found, results[i].err
		switch {
		case err != nil:
			failed++
			fmt.Fprintf(os.Stderr, "FAIL  %s: %v\n", app, err)
		case !found.OK():
			failed++
			fmt.Fprintf(os.Stderr, "FAIL  %s: %s\n", app, strings.Join(found.Problems(), "; "))
		default:
			fmt.Fprintf(os.Stderr, "ok    %s\n", app)
		}
	}

	if failed > 0 {
		return fmt.Errorf("%d app(s) failed lint", failed)
	}
	return nil
}
