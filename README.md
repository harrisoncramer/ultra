# ultra

The `ultra` package takes an opinionated, Go-native approach toward configuration management. The package and CLI provide a system to resolve, inject, parse, and validate secrets. 

Define your app's configuration once, and let Ultra do the rest.

NOTE: This project is in alpha and subject to breaking changes.

## How it works

All secrets start in a secret store (1Password, AWS Secrets Manager), and must end up as an environment variable inside a running container. This is what `ultra` helps with.

There are two parts of the ultra package. 

The first, Ultra's `Config` object, is a thin wrapper around [caarlos0/env](https://github.com/caarlos0/env), which reads and validates your app's configuration. Export your Config (or compose multiple) from a `config` package, and at application startup, call ultra's `Load()` method to expose the secrets:

```go
// Package config defines the app's configuration, loaded from the environment via the shared ultra loader.
package config

import "github.com/harrisoncramer/ultra"

type GoogleConfig struct {
	ClientID     string `env:"GOOGLE_CLIENT_ID,required,notEmpty" secret:"true"`
	ClientSecret string `env:"GOOGLE_CLIENT_SECRET,required,notEmpty" secret:"true"`
	RefreshToken string `env:"GOOGLE_REFRESH_TOKEN,required,notEmpty" secret:"true"`
}

type DatabaseConfig struct {
	URL string `env:"DATABASE_URL,required,notEmpty" secret:"true"`
}

type Config struct {
	Google   GoogleConfig
	Database DatabaseConfig
	LogLevel string `env:"LOG_LEVEL" envDefault:"info"` // Values without the "secret" tag are not loaded from the secret store
}

// Load reads the worker configuration from the environment.
func Load() (Config, error) {
	return ultra.Load[Config]()
}
```

Group related settings into their own structs and compose them. Ultra follows embedded structs, so `GOOGLE_CLIENT_ID` and `DATABASE_URL` above are discovered through `GoogleConfig` and `DatabaseConfig` just as if they were declared inline. Then call `Load()` once at the start of your application:

```go
cfg, err := config.Load()
if err != nil {
	log.Fatal(err)
}

fmt.Println("The Google Client ID is: %s", cfg.Google.ClientID)
```

This configuration object is resolved by the `ultra` CLI tool, which supports different secret resolvers: 1Password, AWS Secret Manager, or a custom secret resolver. 

Secret resolution happens entirely in memory, on demand, so no secrets are written to disk. The `ultra` CLI forwards secrets from the configured secret store into the running container automatically. The docker compose for the above configuration would look like this:

```yaml
services:
  worker:
    build: .
    environment:
      LOG_LEVEL: info
      # GOOGLE_CLIENT_ID, GOOGLE_CLIENT_SECRET, GOOGLE_REFRESH_TOKEN, and
      # DATABASE_URL are secrets; omit them here; ultra resolves and injects
      # them at run time.
```

Finally, run all Docker commands through the `ultra` CLI, so the app's secrets are present in its environment:

```
ultra run 1password --vault MyVault -- docker compose up
```

The Ultra CLI will automatically discover every app under the apps directory (`apps/` by default), reads each app's `config` package, resolve it's secrets via your resolver, and forward them into the container.

## Secret Providers

A provider resolves secret values from a backing store. It implements a single interface:

```go
type Resolver interface {
	Resolve(ctx context.Context, names []string) (map[string]string, error)
}
```

Each resolver is exposed as a subcommand of `run` with its own flags:

```bash
ultra run 1password --vault MyVault -- docker compose up
ultra run aws --region us-east-1 -- docker compose up
```

### Writing a custom resolver

A resolver is any type with a `Resolve` method. To add one, drop a file under `cmd/ultra/resolvers`, implement the interface, and expose it as a `run` subcommand. The runner handles discovery, overrides, namespacing, and exec — your resolver only fetches values.

```go
package resolvers

import (
	"context"
	"fmt"

	"github.com/harrisoncramer/ultra/cmd/ultra/flags"
	"github.com/harrisoncramer/ultra/cmd/ultra/runner"

	"github.com/spf13/cobra"
)

// myStore fetches secrets from wherever you keep them.
type myStore struct {
	app string
	// connection config, populated from flags
}

func (m myStore) Resolve(ctx context.Context, names []string) (map[string]string, error) {
	out := make(map[string]string, len(names))
	for _, name := range names {
		// Look up name for m.app and set out[name]; omit it if the store has no
		// such secret. Return a non-nil error only if the store is unreachable.
	}
	return out, nil
}

func NewMyStoreCmd(shared *flags.SharedFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:  "mystore -- <command>...",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dash := cmd.ArgsLenAtDash()
			if dash < 0 || dash >= len(args) {
				return fmt.Errorf("usage: ultra run mystore -- <command>")
			}
			return runner.Run(cmd.Context(), runner.Params{
				Root:        shared.Root,
				AppsDir:     shared.AppsDir,
				ResolverFor: func(app string) runner.Resolver { return myStore{app: app} },
				Command:     args[dash:],
			})
		},
	}
	return cmd
}
```

Register it in `newRunCmd` (`cmd/ultra/main.go`):

```go
cmd.AddCommand(resolvers.NewMyStoreCmd(shared))
```

`ultra run mystore -- docker compose up` now resolves through your store.
