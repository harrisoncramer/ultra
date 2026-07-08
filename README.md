# ultra

Ultra provides an opinionated and declarative framework to configuring Go apps with Docker.

With ultra, you can inject, parse, and validate secrets and configuration for each containerized application without having to re-define secrets repeatedly. Define your app's configuration once, and let Ultra do the rest.

You define your configuration as normal Go structs and annotate each field with where its value comes from: an environment variable, a secret provider, and so on. During development and in CI, Ultra resolves those values from providers like 1Password or AWS Secrets Manager and validates the result against the same struct, including its validation tags, so missing or malformed configuration fails before your application ever starts.

At runtime your app keeps loading configuration however it already does, whether that's plain environment variables or another loader. Validation, resolution, and runtime injection stay separate concerns, which is what makes Ultra complement tools like Docker Compose, Kubernetes Secrets, and env loaders rather than replace them.

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

Three ideas set Ultra apart from a plain config loader.

Configuration is a contract. Most config libraries answer "how do I load configuration?" Ultra answers "how do I prove my configuration is valid before the app starts?" `ultra validate` resolves every dependency and checks every field against the struct, so it is closer to schema validation than to loading.

Secret providers are first-class. The struct itself records that `DATABASE_URL` comes from 1Password or that `STRIPE_SECRET_KEY` comes from AWS Secrets Manager. Where a value lives is part of the schema, not buried in shell scripts or CI config.

It works with what you already have. Ultra does not replace runtime loading. Keep `os.Getenv`, caarlos0/env, koanf, or Kubernetes Secrets; Ultra is a development-time verification layer on top, so nothing at runtime has to change to adopt it.

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

`gen` is a separate step only when you want the override file without the store, for CI or to commit it. `run` regenerates it itself, so for the plain local loop you can skip straight to it.

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

## Documentation

Concepts and guides:

- [How it works](docs/concepts.md): the secrets-vs-config model and how overrides are generated
- [Commands](docs/commands.md): what `gen`, `run`, `validate`, and `lint` are for
- [Resolvers](docs/resolvers.md): secret and config resolvers, their flags, and the override layer
- [Environments](docs/environments.md): the `required` tag and `--env`
- [Configuration file](docs/config-file.md): the optional `.ultra.toml`
- [Custom resolvers](docs/custom-resolvers.md): adding your own secret or config backend

Command reference, with the usage line and every flag for each command:

- [reference/](docs/reference/ultra.md)
