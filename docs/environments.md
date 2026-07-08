# Environments

Which fields are required can vary by environment. Declare it with the `required` tag: `*` means every environment, a comma-separated list means only those, and no tag means never required. A required field must be set and non-empty in an environment it applies to.

```go
type Config struct {
	DatabaseURL  string `env:"DATABASE_URL" secret:"true" required:"*"`
	SigningKey   string `env:"SIGNING_KEY" secret:"true" required:"production,staging"`
	DevUploadURL string `env:"DEV_UPLOAD_URL" required:"local"`
	Optional     string `env:"OPTIONAL"` // no required tag: never required
}
```

Required-ness lives only in the `required` tag. The env library's own `required`/`notEmpty` options are not used; `Load` rejects them, so there is a single, environment-aware source of truth. A `required` tag on an embedded or nested struct applies to all of its fields, so a group of environment-specific fields can be declared once:

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
