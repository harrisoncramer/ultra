package cli

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/harrisoncramer/ultra/pkg/secrets"
)

type lintParams struct {
	root               string
	apps               []string
	configDir          string
	environment        string
	rejectUnreferenced bool
	secretResolver     func(app string) SecretResolver
	configResolver     ConfigResolver
}

// lintFindings is what lintApp reports for one app: the required keys no resolver
// provides, and — when rejectUnreferenced is set — the keys a resolver provides
// that no Config field references.
type lintFindings struct {
	missing []string
	extra   []string
}

// lint statically checks that every required key each app's Config declares will
// be provided — secrets by the secret resolver, non-secret config by the config
// resolver — by comparing the declared keys against the keys those resolvers
// offer. Unlike validate it never constructs or parses a value, so it runs where
// the real secret values aren't reachable (e.g. CI, with a resolver that reads
// declared keys from deployment manifests). It reports each app and exits
// non-zero if any required key is unprovided.
func lint(ctx context.Context, p lintParams) error {
	failed := 0
	for _, appPath := range p.apps {
		app := appName(appPath)
		found, err := lintApp(ctx, p, appPath)
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

// lintApp reports the required keys app declares that neither resolver provides,
// and — when rejectUnreferenced is set — the keys a resolver provides that no
// Config field references. Secret fields are checked against the secret
// resolver's keys and non-secret fields against the config resolver's; the
// resolved values themselves are ignored, only the presence of each key matters.
func lintApp(ctx context.Context, p lintParams, appPath string) (lintFindings, error) {
	app := appName(appPath)
	fields, err := secrets.Fields(appConfigDir(p.root, appPath, p.configDir))
	if err != nil {
		return lintFindings{}, err
	}

	var secretNames []string
	for _, f := range fields {
		if f.Secret {
			secretNames = append(secretNames, f.Name)
		}
	}

	secretVals := map[string]string{}
	if len(secretNames) > 0 {
		secretVals, err = p.secretResolver(app).Resolve(ctx, secretNames)
		if err != nil {
			return lintFindings{}, err
		}
	}

	configVals, err := p.configResolver.Resolve(ctx, app)
	if err != nil {
		return lintFindings{}, err
	}

	var missing []string
	for _, f := range fields {
		if !f.RequiredIn(p.environment) {
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
	if p.rejectUnreferenced {
		declared := declaredNames(fields)
		extra = append(unreferenced(secretVals, declared), unreferenced(configVals, declared)...)
		sort.Strings(extra)
	}
	return lintFindings{missing: missing, extra: extra}, nil
}

// declaredNames is the set of every env-var name the app's Config references,
// secret and non-secret alike.
func declaredNames(fields []secrets.Field) map[string]struct{} {
	declared := make(map[string]struct{}, len(fields))
	for _, f := range fields {
		declared[f.Name] = struct{}{}
	}
	return declared
}

// unreferenced returns the keys in provided that no declared name covers — the
// values a resolver supplies that the app's Config never reads.
func unreferenced(provided map[string]string, declared map[string]struct{}) []string {
	var extra []string
	for k := range provided {
		if _, ok := declared[k]; !ok {
			extra = append(extra, k)
		}
	}
	return extra
}
