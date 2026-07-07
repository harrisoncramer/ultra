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
	root           string
	apps           []string
	configDir      string
	secretResolver func(app string) SecretResolver
	configResolver ConfigResolver
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
		missing, err := lintApp(ctx, p, appPath)
		switch {
		case err != nil:
			failed++
			fmt.Fprintf(os.Stderr, "FAIL  %s: %v\n", app, err)
		case len(missing) > 0:
			failed++
			fmt.Fprintf(os.Stderr, "FAIL  %s: missing required keys: %s\n", app, strings.Join(missing, ", "))
		default:
			fmt.Fprintf(os.Stderr, "ok    %s\n", app)
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d app(s) failed lint", failed)
	}
	return nil
}

// lintApp returns the required keys app declares that neither resolver provides.
// Secret fields are checked against the secret resolver's keys and non-secret
// fields against the config resolver's; the resolved values themselves are
// ignored, only the presence of each key matters.
func lintApp(ctx context.Context, p lintParams, appPath string) ([]string, error) {
	app := appName(appPath)
	fields, err := secrets.Fields(appConfigDir(p.root, appPath, p.configDir))
	if err != nil {
		return nil, err
	}

	var secretNames []string
	for _, f := range fields {
		if f.Secret {
			secretNames = append(secretNames, f.Name)
		}
	}

	providedSecret := map[string]struct{}{}
	if len(secretNames) > 0 {
		vals, err := p.secretResolver(app).Resolve(ctx, secretNames)
		if err != nil {
			return nil, err
		}
		for k := range vals {
			providedSecret[k] = struct{}{}
		}
	}

	configVals, err := p.configResolver.Resolve(ctx, app)
	if err != nil {
		return nil, err
	}
	providedConfig := map[string]struct{}{}
	for k := range configVals {
		providedConfig[k] = struct{}{}
	}

	var missing []string
	for _, f := range fields {
		if !f.Required {
			continue
		}
		provided := providedConfig
		if f.Secret {
			provided = providedSecret
		}
		if _, ok := provided[f.Name]; !ok {
			missing = append(missing, f.Name)
		}
	}
	sort.Strings(missing)
	return missing, nil
}
