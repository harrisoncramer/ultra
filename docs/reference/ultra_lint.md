## ultra lint

Statically check each app has no required key its resolvers won't provide

### Synopsis

lint checks that every required config key an app declares is provided —
secrets by --secret-resolver, non-secret config by --config-resolver — by
comparing the declared keys against the keys those resolvers offer. Unlike
validate it never parses values or runs the app's config, so it works where
the real secret values aren't reachable, such as CI with a resolver that
reads declared keys from deployment manifests. Apps are the directories
given as arguments, or those in .ultra.toml when none are given. It reports
each app and exits non-zero if any required key is unprovided.

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
      --override-dir string      directory under --root the generated compose override is written to; point it at a committed path to keep it in version control (default "tmp")
      --override-name string     file name of the generated compose override under --override-dir; set it to docker-compose.override.yml to have compose auto-load it (default "ultra.compose.override.yml")
      --reject-unreferenced      fail an app when a resolver provides a key no Config field references
      --root string              repo root the compose file and overrides are anchored to (default ".")
      --secret-resolver string   secret backend: 1password, vault, aws-secret-manager
```

### SEE ALSO

* [ultra](ultra.md)	 - Wire every app's config.go secrets into local docker-compose dev

