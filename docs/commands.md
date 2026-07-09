# Commands

This page explains what each command is for and when to reach for it. For the exact usage line and every flag, see [reference/](reference/ultra.md) or run `ultra <command> --help`.

## run

![ultra run](assets/demos/run.gif)

The `run` command resolves each app's secrets via the selected secret resolver and execs the given command with them set. It regenerates a names-only docker compose override from each app's `Config` into a temporary directory on every run, points `COMPOSE_FILE` at it, and lets docker interpolate the resolved secrets into the containers. The override is derived from the code each time, so it is always current; no separate step, no committed file, and no secret value is written to disk.

```bash
ultra run apps/worker --secret-resolver 1password --vault MyVault -- docker compose up
```

## validate

![ultra validate](assets/demos/validate.gif)

Ultra supports validating a configuration prior to starting a container with the `validate` command. This is helpful in CI, or during local development. The `ultra validate` takes the same secret resolver and flags as `run` and checks that every app's `config.Load` succeeds. It exits non-zero if any app is missing a required value or won't parse.

```bash
ultra validate apps/server apps/worker --secret-resolver aws-secret-manager --region us-east-1
```

The `validate` command also takes an optional `--reject-unreferenced` flag, which tightens the check: on top of failing when a required key is unprovided, it also fails when a resolver provides a key no `Config` field references. That catches the other half of config drift, a stale compose variable or a vault entry nothing consumes.

## lint

![ultra lint](assets/demos/lint.gif)

The `lint` command is a less-strict version of the `validate` command. It takes the same resolvers as `validate` but never constructs or parses a value. It only compares the keys each app's `Config` requires against the keys its resolvers offer, and fails if a required key is unprovided.

While `validate` needs real secret values, because it reconstructs the environment and parses it, `lint` does not. Because it never reads a value, it can run against a resolver that reports declared keys (from deployment manifests, for instance) rather than from the store itself, so no secret access is needed.

```bash
ultra lint apps/server apps/worker --secret-resolver aws-secret-manager --region us-east-1
```

Use `lint` to catch config drift early: a required field added in code but missing from the platform's config or secret declarations. Use `validate` where the real values are available, such as local development or an in-cluster job, where you have access to sensitive data.

The `lint` command also takes the `--reject-unreferenced` flag.

```bash
ultra lint apps/server --secret-resolver aws-secret-manager --region us-east-1 --reject-unreferenced
```

The `lint` command also fails when a `secret`-tagged field has its value hardcoded in the non-secret config, since secrets belong in the store, not committed config.
