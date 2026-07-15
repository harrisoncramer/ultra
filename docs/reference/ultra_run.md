## ultra run

Resolve the given apps' secrets with a secret resolver and exec the command

### Synopsis

The run command resolves each app's secrets from the secret provider and execs
your command with them set. On every run it regenerates a names-only docker
compose override from each app's Config into a temporary directory and points
COMPOSE_FILE at it, so docker interpolates the resolved secrets into your
containers. The override is derived from the code each time, so it is always
current; no secret value is written to disk.

Pass --compose-file more than once to layer compose files, like docker's own -f:
they are set on COMPOSE_FILE in order, so a later file (e.g. a gitignored local
override) wins over an earlier one, while the generated secrets override still
applies on top.

```
ultra run [app-path...] --secret-resolver <name> [flags] -- <command>...
```

### Options

```
      --compose-file stringArray   docker compose file COMPOSE_FILE points at, relative to --root; repeatable, later files win (default "docker-compose.yml")
      --config-dir string          config package directory under each app path (e.g. pkg/config) (default "config")
      --config-file string         path to the ultra config file (default .ultra.toml under --root)
  -h, --help                       help for run
      --root string                repo root the compose file and overrides are anchored to (default ".")
      --secret-resolver string     secret backend: 1password, vault, aws-secret-manager
```

### SEE ALSO

* [ultra](ultra.md)	 - Wire every app's config.go secrets into local docker-compose dev

