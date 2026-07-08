# Commands

This page explains what each command is for and when to reach for it. For the exact usage line and every flag, see the generated reference under [reference/](reference/ultra.md). It is produced from the command definitions themselves, so it never drifts.

## gen: generating the override

The compose override is references only: it maps each declared secret name onto its launcher variable and never contains a value. All the named apps go into one file, one service block per app, written to `--override-dir/--override-name` (by default `tmp/ultra.compose.override.yml`). Generating it is a static operation over each app's `Config` and needs no secret store, so `ultra run` regenerates it on every launch. To produce it without launching, for CI, a setup step, or to commit it into version control, use `ultra gen`:

```bash
ultra gen apps/server apps/worker --override-dir compose/overrides
```

Because `gen` never contacts the store, it works offline, the same property that makes `lint` useful. Point `--override-dir` at a committed path to keep the file in version control; `run` writes to and reads from the same path. The override lists every secret each app declares, so it stays correct as long as the `Config` does, regardless of what the store currently holds.

`run` chains the override onto your base compose file through `COMPOSE_FILE`. If you run `docker compose` by hand instead, either reference it explicitly (`docker compose -f docker-compose.yml -f tmp/ultra.compose.override.yml up`) or pass `--override-name docker-compose.override.yml` so compose auto-loads it next to the base file. The default name avoids that auto-load so it never clobbers a hand-written `docker-compose.override.yml`.

## run

`run` resolves each app's secrets via the selected secret resolver, forwards them into that app's container through a generated compose override, and execs the given command. No secret is written to disk.

```bash
ultra run apps/worker --secret-resolver 1password --vault MyVault -- docker compose up
```

## validate: validating configuration

Ultra supports validating a configuration prior to starting a container. This is helpful in CI, or during local development. The `ultra validate` takes the same secret resolver and flags as `run` and checks that every app's `config.Load` succeeds. It exits non-zero if any app is missing a required value or won't parse.

```bash
ultra validate apps/server apps/worker --secret-resolver aws-secret-manager --region us-east-1
```

## lint: linting configuration

The `ultra validate` needs real secret values, because it reconstructs the environment and parses it, so it must run somewhere the secret store is reachable.

The `ultra lint` command is a less-strict requirement. It takes the same resolvers as `validate` but never constructs or parses a value. It only compares the keys each app's `Config` requires against the keys its resolvers offer, and fails if a required key is unprovided.

Because it never reads a value, it can run against a resolver that reports declared keys (from deployment manifests, for instance) rather than from the store itself, so no secret access is needed.

```bash
ultra lint apps/server apps/worker --secret-resolver aws-secret-manager --region us-east-1
```

Use `lint` to catch config drift early: a required field added in code but missing from the platform's config or secret declarations. Use `validate` where the real values are available, such as local development or an in-cluster job.

Both commands take `--reject-unreferenced`, which tightens the check: on top of failing when a required key is unprovided, it also fails when a resolver provides a key no `Config` field references. That catches the other half of config drift, a stale compose variable or a vault entry nothing consumes.

```bash
ultra lint apps/server --secret-resolver aws-secret-manager --region us-east-1 --reject-unreferenced
```

`lint` also fails when a `secret`-tagged field has its value hardcoded in the non-secret config, since secrets belong in the store, not committed config. When the config resolver is `docker-compose`, it reads the file without interpolation, so an entry that forwards a variable (`API_KEY: ${API_KEY}`) is treated as a reference and passes, while a pasted literal (`API_KEY: sk_live_...`) is flagged. Config sources that can't hold a literal, such as the `env` resolver, are not checked.
