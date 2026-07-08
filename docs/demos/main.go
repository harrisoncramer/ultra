// Command ultra is a demo build of the ultra CLI: the real command tree plus a
// custom "env" secret resolver that reads secrets straight from environment
// variables, so the whole demo runs offline with no secret store to stand up.
package main

import (
	"context"
	"os"

	"github.com/harrisoncramer/ultra/cli"

	"github.com/spf13/pflag"
)

// envResolver resolves an app's secrets from the process environment, keyed by
// each field's env name.
type envResolver struct{}

// Resolve returns the value of each requested name from the environment,
// omitting any that are unset.
func (envResolver) Resolve(_ context.Context, names []string) (map[string]string, error) {
	out := make(map[string]string, len(names))
	for _, name := range names {
		if v, ok := os.LookupEnv(name); ok {
			out[name] = v
		}
	}
	return out, nil
}

func init() {
	cli.RegisterSecretResolver(cli.SecretResolverCommand{
		Name:  "env",
		Short: "Resolve secrets from environment variables",
		Setup: func(_ *pflag.FlagSet) func(app string) cli.SecretResolver {
			return func(string) cli.SecretResolver {
				return envResolver{}
			}
		},
	})
}

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
