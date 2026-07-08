# How it works

Applications need environment variables. Those values come from two places: secrets live in a secret store (1Password, AWS Secrets Manager) and must be resolved and injected without ever touching disk, while non-secret config comes from a platform, like docker compose, or application manifests in production.

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

With ultra, secret resolution happens entirely in memory, on demand, so no secrets are written to disk. The `ultra` CLI forwards secrets from the configured secret store into the running container by generating a single compose override, with one service block per app, that maps every secret each app's `Config` declares onto an app-namespaced launcher variable, then setting those variables at launch for the secrets the store returns. The override lists names only, never values, so it is safe to commit. A secret the store doesn't hold has no launcher variable set, so its override entry interpolates to empty; secrets are expected to come from the store, and a missing one is surfaced by `ultra validate` and `ultra lint`.

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
