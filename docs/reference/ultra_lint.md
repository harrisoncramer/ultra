## ultra lint

Statically check each app has no required key its resolvers won't provide

### Synopsis

The lint command checks that every required config key an app declares is
provided, without resolving or parsing any values. Because it never reads a
value, it works where the real secrets aren't reachable, like CI. It exits
non-zero if a required key is missing. However, it does not validate that the secrets
themselves are present in the secret provider, like validate does.

```
ultra lint [app-path...] --secret-resolver <name> [flags]
```

### Options

```
      --compose-file string      docker compose file to read non-secret config from, relative to --root (default "docker-compose.yml")
      --config-dir string        config package directory under each app path (e.g. pkg/config) (default "config")
      --config-file string       path to the ultra config file (default .ultra.toml under --root)
      --config-resolver string   non-secret config source: docker-compose, env (default "docker-compose")
      --env string               environment to check for; a field's required tag decides whether it's required in it
  -h, --help                     help for lint
      --reject-unreferenced      fail an app when a resolver provides a key no Config field references
      --root string              repo root the compose file and overrides are anchored to (default ".")
      --secret-resolver string   secret backend: 1password, vault, aws-secret-manager
```

### SEE ALSO

* [ultra](ultra.md)	 - Wire every app's config.go secrets into local docker-compose dev

