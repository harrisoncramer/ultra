# ultra

Ultra provides an opinionated and declarative framework to configuring Go apps with Docker.

With ultra, you can inject, parse, and validate secrets and configuration for each containerized application without having to re-define secrets repeatedly. Define your app's configuration once, and let Ultra do the rest.

NOTE: This project is still under development. Expect breaking changes!

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

```env
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
	LogLevel    string `env:"LOG_LEVEL" envDefault:"info"`
	DatabaseURL string `env:"DATABASE_URL" secret:"true" required:"*"`
	StripeKey   string `env:"STRIPE_SECRET_KEY" secret:"true" required:"*"`
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

type Config struct {
	DatabaseURL string `env:"DATABASE_URL" secret:"true" required:"*"`
	LogLevel    string `env:"LOG_LEVEL" envDefault:"info"`
}
```

Load it once at startup with `ultra.Load`, passing a pointer to your config; it parses the environment into it and hands it back:

```go
cfg, err := ultra.Load(&config.Config{})
if err != nil {
    log.Fatal(err)
}
```

3. Generate an app's compose override, validate it, and/or run your command with secrets injected:

```bash
ultra gen apps/worker                                                                          # writes the names-only compose override, no secret store needed
ultra validate apps/worker --secret-resolver 1password --vault Engineering                     # fails fast if a required value is missing, or malformed
ultra run apps/worker --secret-resolver 1password --vault Engineering -- docker compose up     # regenerates the override, injects DATABASE_URL, starts the container
```

`gen` is a separate step only when you want the override files without the store — in CI, or to commit them. `run` regenerates them itself, so for the plain local loop you can skip straight to it.

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

The `ultra` tool models both though a `Config` value, which the Ultra CLI validates using the excellent [caarlos0/env](https://github.com/caarlos0/env) library.

Prior to application startup, call `ultra validate` to verify that your `Config` is valid with all secrets and environment configuration. Call `ultra run` to inject the variables and expose them to your application during runtime. 

Here's what a more complex configuration with Ultra might look like:

```go
package config

type GoogleConfig struct {
	ClientID     string `env:"GOOGLE_CLIENT_ID" secret:"true" required:"*"`
	ClientSecret string `env:"GOOGLE_CLIENT_SECRET" secret:"true" required:"*"`
	RefreshToken string `env:"GOOGLE_REFRESH_TOKEN" secret:"true" required:"*"`
}

type DatabaseConfig struct {
	URL string `env:"DATABASE_URL" secret:"true" required:"*"`
}

type Config struct {
	Google   GoogleConfig
	Database DatabaseConfig
	LogLevel string `env:"LOG_LEVEL" envDefault:"info"` // Values w/out "secret" are config values
}
```

Call `ultra.Load` once at the start of your application, passing a pointer to your config:

```go
cfg, err := ultra.Load(&config.Config{})
if err != nil {
	log.Fatal(err)
}

fmt.Println("The Google Client ID is: %s", cfg.Google.ClientID)
```

With ultra, secret resolution happens entirely in memory, on demand, so no secrets are written to disk. The `ultra` CLI forwards secrets from the configured secret store into the running container by generating, per app, a compose override that maps every secret the app's `Config` declares onto an app-namespaced launcher variable, then setting those variables at launch for the secrets the store returns. The override lists names only, never values, so it is safe to commit. A secret the store doesn't hold has no launcher variable set, so its override entry interpolates to empty; secrets are expected to come from the store, and a missing one is surfaced by `ultra validate` and `ultra lint`. 

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

## Environments

Which fields are required can vary by environment. Declare it with the `required` tag: `*` means every environment, a comma-separated list means only those, and no tag means never required. A required field must be set and non-empty in an environment it applies to.

```go
type Config struct {
	DatabaseURL  string `env:"DATABASE_URL" secret:"true" required:"*"`
	SigningKey   string `env:"SIGNING_KEY" secret:"true" required:"production,staging"`
	DevUploadURL string `env:"DEV_UPLOAD_URL" required:"local"`
	Optional     string `env:"OPTIONAL"` // no required tag: never required
}
```

Required-ness lives only in the `required` tag. The env library's own `required`/`notEmpty` options are not used — `Load` rejects them — so there is a single, environment-aware source of truth. A `required` tag on an embedded or nested struct applies to all of its fields, so a group of environment-specific fields can be declared once:

```go
type Config struct {
	Base                          // required:"*" fields, etc.
	DevConfig  `required:"local"` // all of DevConfig's fields are required only in local
}
```

Tell `Load` which environment it is loading for with `WithEnvironment`. A `required:"*"` field is enforced regardless; a field required in specific environments is enforced only when one is given and matches:

```go
cfg, err := ultra.Load(&config.Config{}, ultra.WithEnvironment("production"))
```

`ultra validate` and `ultra lint` take the environment with `--env`, so each environment enforces only its own required fields:

```bash
ultra validate --secret-resolver aws-secret-manager --env production
ultra lint --secret-resolver externalsecret --env staging
```

## Resolvers

Ultra supports two kinds of resolvers. 

Your **secret resolver** says where secrets come from (1Password, AWS Secrets Manager, etc). 

Your **config resolver** says where an app's non-secret configuration comes from. 

The config resolver is used by `ultra validate` and `ultra lint`; `ultra run` never uses it. See the config resolver section below for what that means and why you rarely need to change it.

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

### Generating overrides

The compose override files are references only: they map each declared secret name onto its launcher variable and never contain a value. Generating them is a static operation over each app's `Config` and needs no secret store, so `ultra run` regenerates them on every launch. To produce them without launching — in CI, a setup step, or to commit them into version control — use `ultra gen`:

```bash
ultra gen apps/server apps/worker --override-dir compose/overrides
```

Because `gen` never contacts the store, it works offline, the same property that makes `lint` useful. Point `--override-dir` at a committed path to keep the files in version control; `run` writes to and reads from the same path. The override lists every secret the app declares, so it stays correct as long as the `Config` does, regardless of what the store currently holds.

### Validating configuration

Ultra supports validating a configuration prior to starting a container. This is helpful in CI, or during local development. The `ultra validate` takes the same secret resolver and flags as `run` and checks that every app's `config.Load` succeeds. It exits non-zero if any app is missing a required value or won't parse.

```bash
ultra validate apps/server apps/worker --secret-resolver aws-secret-manager --region us-east-1
```

### Linting configuration

The `ultra validate` needs real secret values, because it reconstructs the environment and parses it, so it must run somewhere the secret store is reachable.

The `ultra lint` command is a less-strict requirement. It takes the same resolvers as `validate` but never constructs or parses a value. It only compares the _keys_ each app's `Config` requires against the _keys its resolvers offer_, and fails if a required key is unprovided. 

Because it never reads a value, it can run against a resolver that reports declared keys (from deployment manifests, for instance) rather than from the store itself, so no secret access is needed.

```bash
ultra lint apps/server apps/worker --secret-resolver aws-secret-manager --region us-east-1
```

Use `lint` to catch config drift early — a required field added in code but missing from the platform's config or secret declarations — and `validate` where the real values are available, such as local development or an in-cluster job.

Both commands take `--reject-unreferenced`, which tightens the check: on top of failing when a required key is unprovided, it also fails when a resolver provides a key no `Config` field references. That catches the other half of config drift — a stale compose variable or a vault entry nothing consumes.

```bash
ultra lint apps/server --secret-resolver aws-secret-manager --region us-east-1 --reject-unreferenced
```

`lint` also fails when a `secret`-tagged field has its value hardcoded in the non-secret config, since secrets belong in the store, not committed config. When the config resolver is `docker-compose`, it reads the file without interpolation, so an entry that forwards a variable (`API_KEY: ${API_KEY}`) is treated as a reference and passes, while a pasted literal (`API_KEY: sk_live_...`) is flagged. Config sources that can't hold a literal, such as the `env` resolver, are not checked.

### Config resolvers

The `--config-resolver` is used by the `ultra validate` and `ultra lint` commands. It tells them where the non-secret values live: `validate` uses it to rebuild the boot environment, and `lint` uses it to learn which non-secret keys the platform will provide. Pick one with `--config-resolver` (default `docker-compose`):

- `docker-compose` (default) — validating on your host, before `up`. Reads from `docker-compose.yml`, or the file given by `--compose-file`.
- `env` — validate inside a running container or pod, where they're already in the environment.
- custom — read them from somewhere else (e.g. a Kubernetes ConfigMap, to gate a deploy from CI).

In short: only set `custom` if you need to check the configuration for your app before a deploy, where the configuration lives elsewhere, such as in configuration manifests.

Projects with more than one compose file — a sandbox stack, a standalone service like a data lake — set `--compose-file` (relative to `--root`) to point at the right one. The same flag feeds `ultra run`, so a per-project config file targets its own compose file end to end: give each its own `.ultra.<project>.toml` with `compose-file` set, and both validate and run follow it.

```bash
ultra validate --config-file ultra/lake.toml    # lake.toml sets compose-file = "docker-compose.lake.yml"
ultra run --config-file ultra/lake.toml -- docker compose up
```


### Configuration file

To reduce arguments and flags, ultra supports an optional `.ultra.toml` at the repo root. Anything passed on the command line overrides the file. Point ultra at a file elsewhere with `--config-file <path>`; when given, the file must exist.

```toml
apps = ["apps/server", "apps/worker"] # The apps to manage when none are named on the command line, plus shared flags.
root = "."                        # --root: repo root the compose file is anchored to
config-dir = "config"             # --config-dir: config package dir under each app path (e.g. pkg/config)
override-dir = "tmp"              # --override-dir: dir under --root the generated compose overrides are written to; point at a committed path to keep them in version control
compose-file = "docker-compose.yml" # --compose-file: the compose file run and the docker-compose resolver target, relative to --root

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

### Override resolvers

Sometimes you need a value to win over whatever the normal backend returns. The common case is local development: a developer points a secret at a local instance, or supplies their own token, without touching committed config. ultra handles this with an override layer, and the override is just another resolver — any registered secret or config resolver can serve as one.

Declare it in the config file with a `[secrets-override]` or `[config-override]` section. Its resolver runs after the base resolver, and its values win for any name both provide. Because an override reuses a real resolver, it is per-app the same way base resolvers are: a personal 1Password vault, for instance, is looked up per app, so each app can override different values.

```toml
[secrets]
resolver = "aws-secret-manager"   # the team's real store

[secrets.aws-secret-manager]
region = "us-east-1"

[secrets-override]
resolver = "1password"            # my personal vault wins locally

[secrets-override.1password]
vault = "LocalDev"
```

The precedence is base first, then override. For secrets that means the launcher env, then the override on top; for config the base config resolver, then the override. The override resolver's own flags live only in its config-file sub-table, never on the command line, so pointing the override at the same provider as the base (a second vault, a different account) never collides with the base resolver's flags.

### Writing a custom secret resolver

You don't fork ultra to add a backend. Import `github.com/harrisoncramer/ultra/cli`, register a secret resolver, and call `cli.Execute` from your own `main` function.

The built-in resolvers live in their own subpackages under `cli/resolvers` and register themselves when imported. The default `ultra` binary blank-imports all of them, but a custom `main` can import only the ones it needs, so a binary carries just those backends and their dependencies. Importing only 1password, for example, leaves the AWS SDK out of the build entirely.

```go
import (
	"github.com/harrisoncramer/ultra/cli"

	_ "github.com/harrisoncramer/ultra/cli/resolvers/onepassword"
)
```

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

The default `docker-compose` resolver is what you want on your own machine. But `validate` is also useful as a pre-deploy gate: before shipping to staging or production, check that the app's `Config` still parses against the non-secret config that environment will actually give it. In Kubernetes, for instance, that config may live in a ConfigMap, where the custom resolver reads it.

Register one with `cli.RegisterConfigResolver` and select it with `--config-resolver`. Here it reads the `data` block of the ConfigMap that provides the app's env in a given environment:

```go
cli.RegisterConfigResolver(cli.ConfigResolverCommand{
	Name:  "configmap",
	Short: "Read non-secret config from a Kubernetes ConfigMap",
	Setup: func(fs *pflag.FlagSet) func(root string) (cli.ConfigResolver, error) {
		var env, dir string
		fs.StringVar(&env, "env", "", "environment whose ConfigMap to read")
		fs.StringVar(&dir, "manifests", "deploy", "directory holding <app>/<env>/configmap.yaml")
		return func(root string) (cli.ConfigResolver, error) {
			if env == "" {
				return nil, fmt.Errorf("configmap resolver requires an env")
			}
			return configMap{root: root, dir: dir, env: env}, nil
		}
	},
})

// configMap reads an app's non-secret env from the data block of the ConfigMap
// that provides it in the target environment.
type configMap struct{ root, dir, env string }

func (c configMap) Resolve(ctx context.Context, app string) (map[string]string, error) {
	path := filepath.Join(c.root, c.dir, app, c.env, "configmap.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading configmap for %s: %w", app, err)
	}
	var doc struct {
		Data map[string]string `yaml:"data"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return doc.Data, nil
}
```

Given a manifest like this, `Resolve` returns `LOG_LEVEL` and `DATABASE_HOST` — the values the platform sets alongside the secrets ultra resolves:

```yaml
# deploy/worker/staging/configmap.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: worker
data:
  LOG_LEVEL: info
  DATABASE_HOST: db.staging.internal
```

To configure multiple environments:

```toml
# .ultra.staging.toml
[config]
resolver = "configmap"

[config.configmap]
env = "staging"
```

```toml
# .ultra.production.toml
[config]
resolver = "configmap"

[config.configmap]
env = "production"
```

Then each environment is a configuration file:

```
ultra validate --config-file .ultra.staging.toml
ultra validate --config-file .ultra.production.toml
```
