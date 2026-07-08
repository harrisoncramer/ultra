# Configuration file

To reduce arguments and flags, ultra supports an optional `.ultra.toml` at the repo root. Anything passed on the command line overrides the file. Point ultra at a file elsewhere with `--config-file <path>`; when given, the file must exist.

```toml
apps = ["apps/server", "apps/worker"] # The apps to manage when none are named on the command line, plus shared flags.
root = "."                        # --root: repo root the compose file is anchored to
config-dir = "config"             # --config-dir: config package dir under each app path (e.g. pkg/config)
output-dir = "tmp"                # --output-dir: dir under --root the generated compose file is written to; point at a committed path to keep it in version control
output-filename = "ultra.compose.yml" # --output-filename: file name of the generated compose file; set to docker-compose.override.yml for compose to auto-load it
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

With a file in place, both commands shrink to the following, with apps and resolver coming from the file:

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

See [resolvers.md](resolvers.md) for the override layer (`[secrets-override]` / `[config-override]`) and per-environment config files.
