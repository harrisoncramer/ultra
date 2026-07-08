//go:build integration

// Command harness is an ultra CLI built for the integration tests. It is the
// real command tree plus a "file" secret resolver that reads secrets from a JSON
// file, so the suite can drive run, validate and lint end to end without a live
// secret store. It is behind the integration build tag so it never ships in the
// default build.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/harrisoncramer/ultra/cli"

	"github.com/spf13/pflag"
)

// fileResolver resolves an app's secrets from a JSON file shaped as
// app -> name -> value.
type fileResolver struct {
	path string
	app  string
}

// Resolve returns the requested names present in the file for this app.
func (f fileResolver) Resolve(_ context.Context, names []string) (map[string]string, error) {
	data, err := os.ReadFile(f.path)
	if err != nil {
		return nil, fmt.Errorf("reading secrets file: %w", err)
	}
	var byApp map[string]map[string]string
	if err := json.Unmarshal(data, &byApp); err != nil {
		return nil, fmt.Errorf("parsing secrets file: %w", err)
	}
	have := byApp[f.app]
	out := make(map[string]string, len(names))
	for _, n := range names {
		if v, ok := have[n]; ok {
			out[n] = v
		}
	}
	return out, nil
}

func init() {
	cli.RegisterSecretResolver(cli.SecretResolverCommand{
		Name:  "file",
		Short: "Integration-test resolver reading secrets from a JSON file",
		Setup: func(fs *pflag.FlagSet) func(app string) cli.SecretResolver {
			path := fs.String("secrets-file", "", "JSON file of app -> name -> value")
			return func(app string) cli.SecretResolver {
				return fileResolver{path: *path, app: app}
			}
		},
	})
}

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
