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

Then call `Load()` once at the start of your application:

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
ultra run --secret-resolver 1password --vault MyVault -- docker compose up
```

The Ultra CLI automatically discovers every app under the apps directory (`apps/` by default), reads each app's `config` package, resolves its secrets via your secret resolver, and forwards them into the container.

## Resolvers

ultra has two symmetric kinds of resolver. A **secret resolver** says where secrets come from (1Password, AWS Secrets Manager, …). A **config resolver** says where an app's non-secret configuration comes from (docker-compose locally, the process env in a running container or pod). Each implements a single method:

```go
type SecretResolver interface {
	Resolve(ctx context.Context, names []string) (map[string]string, error)
}

type ConfigResolver interface {
	Resolve(ctx context.Context, app string) (map[string]string, error)
}
```

The secret resolver is chosen with `--secret-resolver`, mirroring `--config-resolver`; the selected resolver's own flags (like `--vault`) become available:

```bash
ultra run --secret-resolver 1password --vault MyVault -- docker compose up
ultra run --secret-resolver aws-secret-manager --region us-east-1 -- docker compose up
```

At runtime `run` doesn't need a config resolver — docker-compose (dev) and your platform (prod) inject the non-secret values into the container directly. The config resolver exists for `validate`, which has to reconstruct that environment itself.

### Validating configuration

`ultra validate` takes the same secret resolver and flags as `run` and resolves secrets the same way, but instead of starting containers it checks that every app's `config.Load` succeeds. It validates against the full environment the container would boot with: the app's non-secret config from a config resolver (`--config-resolver`, `docker-compose` by default) plus the resolved secrets. It exits non-zero if any app is missing a required value or won't parse.

```bash
ultra validate --secret-resolver aws-secret-manager --region us-east-1
```

Use it to fail fast before `docker compose up`, or to gate a deploy. In a running container or pod, where the non-secret values are already in the environment, use `--config-resolver env`.

### Configuration file

Every flag can be prebaked in an optional `.ultra.toml` at the repo root, so you don't repeat `--secret-resolver`, `--region`, and friends on every invocation. Anything passed on the command line overrides the file.

The file mirrors the CLI's hierarchy. Shared flags sit at the top level. A `[secrets]` section picks the secret resolver, and that resolver's own flags live in a sub-table keyed by its name — exactly as `--secret-resolver aws-secret-manager` is followed by aws-secret-manager's own `--region`/`--profile` flags on the command line. `[config]` works the same way.

```toml
# Shared flags live at the top level.
root     = "."          # --root: repo root the compose file is anchored to
apps-dir = "services"   # --apps-dir: directory holding each app's config package

[secrets]
resolver = "aws-secret-manager"   # --secret-resolver

[secrets.aws-secret-manager]      # aws-secret-manager's own flags
region  = "us-east-1"
profile = "prod"
prefix  = "prod"                  # leading segment before <app>/<NAME>

[config]
resolver = "docker-compose"       # --config-resolver
```

Only the selected resolver's sub-table is read, so you can keep several resolvers configured side by side and switch between them by changing one line. Swapping to 1Password is just:

```toml
[secrets]
resolver = "1password"            # --secret-resolver

[secrets.1password]
vault = "Engineering"             # 1password's own flag
```

With a file in place, both commands shrink to:

```bash
ultra run -- docker compose up
ultra validate
```

The file is optional; with no `.ultra.toml`, pass everything on the command line as before.

### Writing a custom secret resolver

You don't fork ultra to add a backend. Import `github.com/harrisoncramer/ultra/cli`, register a secret resolver, and call `cli.Execute` from your own `main` — the built-in resolvers come along, and yours becomes another `--secret-resolver` choice.

A secret resolver is any type with a `Resolve` method. `Setup` binds the resolver's flags and returns a factory that builds a resolver per app once those flags are parsed.

```go
package main

import (
	"context"
	"os"

	"github.com/harrisoncramer/ultra/cli"

	"github.com/spf13/pflag"
)

func main() {
	cli.RegisterSecretResolver(cli.SecretResolverCommand{
		Name:  "vault",
		Short: "Resolve secrets from HashiCorp Vault",
		Setup: func(fs *pflag.FlagSet) func(app string) cli.SecretResolver {
			var addr string
			fs.StringVar(&addr, "addr", "", "vault address")
			return func(app string) cli.SecretResolver {
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

Build that `main` (`go install .`) and run it in place of the stock ultra binary. `ultra run --secret-resolver vault --addr … -- docker compose up` resolves through your store, and the built-in resolvers still work. The CLI handles discovery, overrides, namespacing, and exec — your resolver only fetches values.

### Writing a custom config resolver

Config resolvers are pluggable the same way, for when `validate` needs to read non-secret config from a source ultra doesn't ship — your Kubernetes manifests, for instance. Register one with `cli.RegisterConfigResolver` and select it with `--config-resolver`:

```go
cli.RegisterConfigResolver(cli.ConfigResolverCommand{
	Name:  "k8s",
	Short: "Read non-secret config from Kubernetes manifests",
	New: func(root string) (cli.ConfigResolver, error) {
		return k8sConfig{root: root}, nil
	},
})

// k8sConfig reads an app's non-secret environment from wherever your config lives.
type k8sConfig struct{ root string }

func (k k8sConfig) Resolve(ctx context.Context, app string) (map[string]string, error) {
	// return app's non-secret environment (name -> value)
	return nil, nil
}
```

Then `ultra validate <secret-resolver> --config-resolver k8s` validates against that source. ultra ships `docker-compose` and `env`; anything platform-specific is a custom config resolver you register, exactly like a custom secret resolver.
