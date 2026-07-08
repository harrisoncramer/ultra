## ultra gen

Generate the single compose override for the given apps without resolving secrets

### Synopsis

gen writes one names-only docker compose override — the file that maps every
secret each app's Config declares onto its namespaced launcher variable, one
service block per app — into --override-dir/--override-name. It reads only the
apps' config packages and never contacts the secret store, so it works offline
and its output can be committed and reused by run. Apps are the directories
given as arguments, or those listed in .ultra.toml when none are given.

```
ultra gen [app-path...] [flags]
```

### Options

```
      --config-dir string      config package directory under each app path (e.g. pkg/config) (default "config")
      --config-file string     path to the ultra config file (default .ultra.toml under --root)
  -h, --help                   help for gen
      --override-dir string    directory under --root the generated compose override is written to; point it at a committed path to keep it in version control (default "tmp")
      --override-name string   file name of the generated compose override under --override-dir; set it to docker-compose.override.yml to have compose auto-load it (default "ultra.compose.override.yml")
      --root string            repo root the compose file and overrides are anchored to (default ".")
```

### SEE ALSO

* [ultra](ultra.md)	 - Wire every app's config.go secrets into local docker-compose dev

