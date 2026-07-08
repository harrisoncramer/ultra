## ultra run

Resolve the given apps' secrets with a secret resolver and exec the command

### Synopsis

run resolves each app's secrets via the secret resolver named by
--secret-resolver (for example 1password), forwards them into that app's
container through a generated compose override, and execs the given command.
Apps are the directories given before --, each holding a config package (name
taken from the path's last element); if none are given the apps listed in
.ultra.toml are used. No secret is written to disk.

```
ultra run [app-path...] --secret-resolver <name> [flags] -- <command>...
```

### Options

```
      --compose-file string      base docker compose file COMPOSE_FILE points at, relative to --root (default "docker-compose.yml")
      --config-dir string        config package directory under each app path (e.g. pkg/config) (default "config")
      --config-file string       path to the ultra config file (default .ultra.toml under --root)
  -h, --help                     help for run
      --override-dir string      directory under --root the generated compose override is written to; point it at a committed path to keep it in version control (default "tmp")
      --override-name string     file name of the generated compose override under --override-dir; set it to docker-compose.override.yml to have compose auto-load it (default "ultra.compose.override.yml")
      --root string              repo root the compose file and overrides are anchored to (default ".")
      --secret-resolver string   secret backend: 1password, vault, aws-secret-manager
```

### SEE ALSO

* [ultra](ultra.md)	 - Wire every app's config.go secrets into local docker-compose dev

