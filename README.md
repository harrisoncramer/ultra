# ultra

Ultra provides an opinionated and declarative framework to configuring Go apps with Docker.

With ultra, you can inject, parse, and validate secrets and configuration for each containerized application without having to re-define secrets repeatedly. Define your app's configuration once, and let Ultra do the rest.

NOTE: This project is still under development. Expect breaking changes.

## Why ultra

A containerized Go app needs its configuration as environment variables. The usual way to get them there is a docker-compose service that enumerates every variable:

```yaml
# docker-compose.yml
services:
  worker:
    build: .
    environment:
      LOG_LEVEL: info
      DATABASE_URL: ${DATABASE_URL}
      STRIPE_SECRET_KEY: ${STRIPE_SECRET_KEY}
      GOOGLE_CLIENT_ID: ${GOOGLE_CLIENT_ID}
      GOOGLE_CLIENT_SECRET: ${GOOGLE_CLIENT_SECRET}
```

Then a secret file:

```bash
LOG_LEVEL=info
DATABASE_URL=postgres://user:pass@db:5432/app
STRIPE_SECRET_KEY=sk_live_...
GOOGLE_CLIENT_ID=...
GOOGLE_CLIENT_SECRET=...
```

This works, but it has drawbacks.

First, every variable is declared in three places: the struct your app parses (or `os.Getenv` calls), the Docker compose `environment:` block, and the `.env` file or environment system. Adding a secret means editing all three, and over time they drift out of sync.

Second, the secrets often leak to disk. An `.env` file may hold plaintext credentials, which leak through backups, accidental commits, or shell history.

Lastly, nothing is validated until the container boots. Missing or malformed values can slip through development processes, and surface as runtime crash rather than an error at boot.

Ultra removes the duplication and the disk. Your typed config is the single source of truth, and fields tagged `secret:"true"` are the secrets:

```go
type Config struct {
	LogLevel     string `env:"LOG_LEVEL" envDefault:"info"`
	DatabaseURL  string `env:"DATABASE_URL,required,notEmpty" secret:"true"`
	StripeKey    string `env:"STRIPE_SECRET_KEY,required,notEmpty" secret:"true"`
}
```

Ultra reads that struct, resolves the secret-tagged values from your store (1Password, AWS Secrets Manager, Vault) entirely in memory, and injects them into the container at run time. Nothing is written to disk. The compose file keeps only the non-secret config, stated explicitly:

```yaml
# docker-compose.yml
services:
  worker:
    build: .
    environment:
      LOG_LEVEL: info
```

And `ultra validate` checks every app's config against the full environment it would boot with, so a missing secret or an unparseable value fails fast instead of at container start.

## Quickstart

1. Install the CLI:

```bash
go install github.com/harrisoncramer/ultra/cmd/ultra@latest
```

2. Create a `config` package for each application using ultra:

```go
package config

import "github.com/harrisoncramer/ultra"

type Config struct {
	DatabaseURL string `env:"DATABASE_URL,required,notEmpty" secret:"true"`
	LogLevel    string `env:"LOG_LEVEL" envDefault:"info"`
}

func Load() (Config, error) {
    return ultra.Load[Config]()
}
```

3. Validate an app, and/or run your command with secrets injected:

```bash
ultra validate apps/worker --secret-resolver 1password --vault Engineering                     # fails fast if a required value is missing
ultra run apps/worker --secret-resolver 1password --vault Engineering -- docker compose up     # injects DATABASE_URL and starts the container
```

4. Optional: add an `.ultra.toml` at the repo root, naming your apps and secret store, so you can drop the flags:

```toml
apps = ["apps/worker"]

[secrets]
resolver = "1password"

[secrets.1password]
vault = "Engineering"
```

You can then just run:

```bash
ultra validate
ultra run -- docker compose up
```

## How it works

Applications need environment variables. Those values come from two places: _secrets_ live in a secret store (1Password, AWS Secrets Manager) and must be resolved and injected without ever touching disk, while _non-secret config_ comes from a platform, like docker compose, or application manifests in production. 

The `ultra` tool models both though a `Config` value, which the Ultra CLI validates. 

Prior to application startup, call `ultra validate` to verify that your `Config` is valid with all secrets and environment configuration. Call `ultra run` to inject the variables and expose them to your application during runtime. 

Here's what a more complex configuration with Ultra might look like:

```go
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
	LogLevel string `env:"LOG_LEVEL" envDefault:"info"` // Values w/out "secret" are config values
}

// Load reads the worker configuration from the environment.
func Load() (Config, error) {
	return ultra.Load[Config]()
}
```

Call `Load()` once at the start of your application:

```go
cfg, err := config.Load()
if err != nil {
	log.Fatal(err)
}

fmt.Println("The Google Client ID is: %s", cfg.Google.ClientID)
```

With ultra, secret resolution happens entirely in memory, on demand, so no secrets are written to disk. The `ultra` CLI forwards secrets from the configured secret store into the running container automatically by building a dynamic set of key/value pairs for each Docker container. 

The docker compose for the above configuration does not need to re-enumerate the secrets stored in the existing `Config` value. This is sufficient: 

```yaml
services:
  worker:
    build: .
    environment:
      LOG_LEVEL: info
```

Run all Docker commands through the `ultra` CLI, so the app's secrets are present in its environment:

```bash
ultra run apps/worker --secret-resolver 1password --vault MyVault -- docker compose up
```

## Resolvers

Ultra supports two kinds of resolvers. 

Your **secret resolver** says where secrets come from (1Password, AWS Secrets Manager, etc). 

Your **config resolver** says where an app's non-secret configuration comes from. 

The config resolver is used only by `ultra validate` and `ultra run` never uses it. See the config resolver section below for what that means and why you rarely need to change it.

```go
type SecretResolver interface {
	Resolve(ctx context.Context, names []string) (map[string]string, error)
}

type ConfigResolver interface {
	Resolve(ctx context.Context, app string) (map[string]string, error)
}
```

Choose your secret resolver with the `--secret-resolver` flag. Secret resolvers take different arguments. Currently, ultra ships with support for AWS Secrets Manager, 1Password, and HashiCorp Vault:

```bash
ultra run apps/worker --secret-resolver 1password --vault MyVault -- docker compose up
ultra run apps/server apps/worker --secret-resolver aws-secret-manager --region us-east-1 -- docker compose up
ultra run apps/worker --secret-resolver vault --mount secret -- docker compose up
```

### Validating configuration

Ultra supports validating a configuration prior to starting a container. This is helpful in CI, or during local development. The `ultra validate` takes the same secret resolver and flags as `run` and checks that every app's `config.Load` succeeds. It exits non-zero if any app is missing a required value or won't parse.

```bash
ultra validate apps/server apps/worker --secret-resolver aws-secret-manager --region us-east-1
```

### Config resolvers

The `--config-resolver` is only ever used by the `ultra validate` command. The resolver just tells `validate` where the non-secret values live, so it can rebuild the boot environment. Pick one with `--config-resolver` (default `docker-compose`):

- `docker-compose` (default) — validating on your host, before `up`. Reads from `docker-compose.yml`
- `env` — validate inside a running container or pod, where they're already in the environment.
- custom — read them from somewhere else (e.g. a Kubernetes ConfigMap, to gate a deploy from CI).

In short: only set `custom` if you need to validate the configuration for your app before a deploy, where the configuration lives elsewhere, such as in configuration manifests.


### Configuration file

To reduce arguments and flags, ultra supports an optional `.ultra.toml` at the repo root. Anything passed on the command line overrides the file.

```toml
apps = ["apps/server", "apps/worker"] # The apps to manage when none are named on the command line, plus shared flags.
root = "."                        # --root: repo root the compose file is anchored to
config-dir = "config"             # --config-dir: config package dir under each app path (e.g. pkg/config)

[secrets]
resolver = "aws-secret-manager"   # --secret-resolver

[secrets.aws-secret-manager]      # aws-secret-manager's own flags
region  = "us-east-1"
profile = "prod"
prefix  = "prod"                  # leading segment before <app>/<NAME>

[config]
resolver = "docker-compose"       # --config-resolver, optional when running locally
```

With a file in place, both commands shrink to — apps and resolver coming from the file:

```bash
ultra run -- docker compose up
ultra validate
```

Only the selected resolver's sub-table is read, so you can keep several resolvers configured side by side. Swapping to 1Password is just:

```toml
[secrets]
resolver = "1password"            # --secret-resolver

[secrets.1password]
vault = "Engineering"             # 1password's own flag
```

Naming apps on the command line overrides the file's `apps` list, so you can still target one at a time: `ultra validate apps/worker`.

### Writing a custom secret resolver

You don't fork ultra to add a backend. Import `github.com/harrisoncramer/ultra/cli`, register a secret resolver, and call `cli.Execute` from your own `main` function.

If you'd like to suggest a new backend, please open a PR and I'll consider shipping it with the ultra core:

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

### Writing a custom config resolver

Config resolvers are pluggable the same way, for when `validate` needs to read non-secret config ship, like Kubernetes manifests, for instance. 

Register one with `cli.RegisterConfigResolver` and select it with `--config-resolver`:

```go
cli.RegisterConfigResolver(cli.ConfigResolverCommand{
	Name:  "k8s",
	Short: "Read non-secret config from Kubernetes manifests",
	New: func(root string) (cli.ConfigResolver, error) {
		return k8sConfig{root: root}, nil
	},
})

type k8sConfig struct{ root string }

func (k k8sConfig) Resolve(ctx context.Context, app string) (map[string]string, error) {
	return nil, nil
}
```

Then `ultra validate --secret-resolver <name> --config-resolver k8s` validates against that source.
