## ultra validate

Resolve the given apps' secrets and config and validate each app's Config

### Synopsis

The validate command checks that each app boots with a complete config. It is
a superset of lint: it runs lint's static checks, then reconstructs the
environment the app would start with and confirms the app's Config parses. It
exits non-zero if any check fails or the config won't parse.

```
ultra validate [app-path...] --secret-resolver <name> [flags]
```

### Options

```
      --compose-file string      docker compose file to read non-secret config from, relative to --root (default "docker-compose.yml")
      --config-dir string        config package directory under each app path (e.g. pkg/config) (default "config")
      --config-file string       path to the ultra config file (default .ultra.toml under --root)
      --config-resolver string   non-secret config source: docker-compose, env (default "docker-compose")
      --env string               environment to check for; a field's required tag decides whether it's required in it
  -h, --help                     help for validate
      --reject-unreferenced      fail an app when a resolver provides a key no Config field references
      --root string              repo root the compose file and overrides are anchored to (default ".")
      --secret-resolver string   secret backend: 1password, vault, aws-secret-manager
```

### SEE ALSO

* [ultra](ultra.md)	 - Wire every app's config.go secrets into local docker-compose dev

