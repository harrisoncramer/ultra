# ultra

Ultra provides an opinionated and declarative framework for configuring Go apps with Docker.

Most configuration tools answer one question: "How do I load configuration?" 

Ultra answers a different question: "How do I validate that my application has the configuration it needs before it starts?"

With ultra, you can inject, parse, and validate secrets and configuration for each containerized application without having to re-define those secrets repeatedly. Define your app's configuration once, and let Ultra do the rest.

NOTE: This project is still under development. Expect breaking changes!

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

Ultra removes the duplication and the disk. Your typed config is the single source of truth, and fields tagged `secret:"true"` are the secrets.

Define your configuration as normal Go structs and annotate each field with where its value comes from: an environment variable, a secret provider, and so on. During development and in CI, Ultra resolves those values from providers like 1Password or AWS Secrets Manager and validates the result against the same struct, including its validation tags, so missing or malformed configuration fails before your application ever starts.

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

Ultra is also capable via `ultra validate` of checking whether every app's config is valid against the full environment it would boot with, so a missing secret or an unparseable value fails fast instead of at container start. Configuration is a contract. Your app likely already has an implicit config schema, spread across its structs, `os.Getenv` calls, and deployment files. Ultra makes that schema explicit. The `ultra validate` command resolves every dependency and checks every field against the struct, so it is closer to schema validation than to loading. Ultra does not replace runtime loading in production. Keep `os.Getenv`, caarlos0/env, koanf, or Kubernetes Secrets; Ultra is a development-time verification layer on top, so nothing at runtime has to change to adopt it.

Ultra is deliberately narrow. It does not try to be Kubernetes, Terraform, or your cloud platform, and it does not want to generate your production infrastructure. Those systems own networking, IAM, secret rotation, provisioning, and deployment strategy, and they are good at it.

Ultra owns one thing: the contract between an application and the configuration it requires. The struct says "this is what my app needs"; the platform decides how those needs get met. That split keeps ownership boundaries clear, and it is the same split other tools already draw. Protocol Buffers define an API without dictating how it is served. OpenAPI describes a service without running it. Database schemas describe the shape of data without choosing the storage engine. Ultra applies that idea to application configuration: describe the requirements, validate them everywhere, and leave fulfillment to the systems built for it.

So Ultra is not a replacement for config loaders or infrastructure tooling. It is the missing piece between them, a typed and validated contract that both sides can agree on.

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
