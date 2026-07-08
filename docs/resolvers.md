# Resolvers

Ultra supports two kinds of resolvers.

Your secret resolver says where secrets come from (1Password, AWS Secrets Manager, etc).

Your config resolver says where an app's non-secret configuration comes from.

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

## Secret resolver flags

Each secret resolver takes its own flags, passed alongside `--secret-resolver`.

1password:

```text
--vault    1password vault holding the secrets (required)
```

aws-secret-manager:

```text
--region   AWS region (defaults to the SDK's configured region)
--prefix   path segment prepended before the app, e.g. an environment name
--profile  named AWS profile to use (defaults to the SDK's credential chain)
```

vault:

```text
--mount      KV v2 mount path the app's secret lives under (default "secret")
--address    Vault address (defaults to VAULT_ADDR)
--namespace  Vault namespace (Enterprise; defaults to VAULT_NAMESPACE)
```

## Config resolvers

The `--config-resolver` is used by the `ultra validate` and `ultra lint` commands. It tells them where the non-secret values live: `validate` uses it to rebuild the boot environment, and `lint` uses it to learn which non-secret keys the platform will provide. Pick one with `--config-resolver` (default `docker-compose`):

- `docker-compose` (default): validating on your host, before `up`. Reads from `docker-compose.yml`, or the file given by `--compose-file`.
- `env`: validate inside a running container or pod, where they're already in the environment.
- custom: read them from somewhere else, such as a Kubernetes ConfigMap to gate a deploy from CI. See [custom-resolvers.md](custom-resolvers.md).

In short: only set `custom` if you need to check the configuration for your app before a deploy, where the configuration lives elsewhere, such as in configuration manifests.

A project with more than one compose file, such as a sandbox stack or a data lake service, sets `--compose-file` relative to `--root` to point at the right one. The same flag feeds `ultra run`, so a per-project config file targets its own compose file end to end: give each its own `.ultra.<project>.toml` with `compose-file` set, and both validate and run follow it.

```bash
ultra validate --config-file ultra/lake.toml    # lake.toml sets compose-file = "docker-compose.lake.yml"
ultra run --config-file ultra/lake.toml -- docker compose up
```

## Override resolvers

Sometimes you need a value to win over whatever the normal backend returns. The common case is local development: a developer points a secret at a local instance, or supplies their own token, without touching committed config. ultra handles this with an override layer. The override is just another resolver: any registered secret or config resolver can serve as one.

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
