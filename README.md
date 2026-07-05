# ultra

The `ultra` package takes an opinionated, Go-native approach toward configuration management with docker and docker compose. 

It handles the injection of secrets into containers automatically. Define your app's configuration once, and let Ultra do the rest.

NOTE: This project is in Alpha and subject to breaking changes.

## How it works

All secrets start in a secret store (1Password, AWS Secrets Manager), and must end up as an environment variable inside a running container. This is what `ultra` helps with.

There are two parts of the ultra package. The first, Ultra's `Config` object, is a thin wrapper around [https://github.com/caarlos0/env](caarlos0/env), which reads and validates your app's configuration. Export your Config (or compose multiple) from a `config` package, and at application startup, call the `Load()` method to expose the secrets:

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

Group related settings into their own structs and compose them. ultra follows embedded and nested structs when it reads your config, so `GOOGLE_CLIENT_ID` and `DATABASE_URL` above are discovered through `GoogleConfig` and `DatabaseConfig` just as if they were declared inline.

Then call `Load()` once at the start of your application:

```go
cfg, err := config.Load()
if err != nil {
	log.Fatal(err)
}

fmt.Println("The Google Client ID is: %s", cfg.Google.ClientID)
```

This configuration object is resolved by the `ultra` CLI tool, which supports different secret resolvers like 1Password, AWS Secret Manager, or a custom secret resolver. Secret resolution happens entirely in memory, on demand, so no secrets are written to disk. The `ultra` CLI forwards secrets from the configured secret store into the running container automatically. Fields without the `secret` struct annotation will be read from the `docker compose` file, and not resolved from your secret store, allowing you to colocate secret and non-secret configuration values.

The docker compose for the above configuration would look like this:

```yaml
services:
  worker:
    build: .
    environment:
      LOG_LEVEL: info
      # GOOGLE_CLIENT_ID, GOOGLE_CLIENT_SECRET, GOOGLE_REFRESH_TOKEN, and
      # DATABASE_URL are secrets; omit them here — ultra resolves and injects
      # them at run time.
```

Finally, run all Docker commands through the `ultra` CLI, so the app's secrets are present in its environment:

```
ultra run --onepassword-vault MyVault -- docker compose up
```

The CLI discovers every app under the apps directory — `apps/` by default, configurable with `--apps-dir` — reads each app's `config` package, resolves its secrets from the provider, and forwards them into that app's container.

## Secret Providers

A provider resolves secret values from a backing store. It implements a single interface:

```go
type Resolver interface {
	Resolve(ctx context.Context, names []string) (map[string]string, error)
}
```

The ultra package ships with a 1Password provider, used by default

Additional stores fit behind the same interface. Implementing `Resolver` for a service such as AWS Secrets Manager is all that a new backend requires.
