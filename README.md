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

This configuration object is resolved by the `ultra` CLI tool, which supports different secret resolvers: 1Password, AWS Secrets Manager, or a custom secret resolver. 

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
ultra run aws-secret-manager --region us-east-1 -- docker compose up
```

### Writing a custom resolver

You don't fork ultra to add a backend. Import `github.com/harrisoncramer/ultra/cli`, register a resolver, and call `cli.Execute` from your own `main` — the built-in resolvers come along, and yours becomes another `run` subcommand.

A resolver is any type with a `Resolve` method. `Setup` binds the resolver's flags and returns a factory that builds a resolver per app once those flags are parsed.

```go
package main

import (
	"context"
	"os"

	"github.com/harrisoncramer/ultra/cli"

	"github.com/spf13/pflag"
)

func main() {
	cli.RegisterResolver(cli.ResolverCommand{
		Name:  "vault",
		Short: "Resolve secrets from HashiCorp Vault",
		Setup: func(fs *pflag.FlagSet) func(app string) cli.Resolver {
			var addr string
			fs.StringVar(&addr, "addr", "", "vault address")
			return func(app string) cli.Resolver {
				return vaultResolver{addr: addr, app: app}
			}
		},
	})
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}

// vaultResolver fetches secrets from wherever you keep them.
type vaultResolver struct {
	addr string
	app  string
}

func (v vaultResolver) Resolve(ctx context.Context, names []string) (map[string]string, error) {
	out := make(map[string]string, len(names))
	for _, name := range names {
		// Look up name for v.app and set out[name]; omit it if the store has no
		// such secret. Return a non-nil error only if the store is unreachable.
	}
	return out, nil
}
```

Build that `main` (`go install .`) and run it in place of the stock ultra binary. `ultra run vault --addr … -- docker compose up` resolves through your store, and `1password` and `aws` still work. The CLI handles discovery, overrides, namespacing, and exec — your resolver only fetches values.
