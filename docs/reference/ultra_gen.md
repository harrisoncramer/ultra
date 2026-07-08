## ultra gen

Generate the single compose file for the given apps without resolving secrets

### Synopsis

gen writes one names-only docker compose file, mapping every secret each app's
Config declares onto its namespaced launcher variable, one service block per
app, into --output-dir/--output-filename. It reads only the apps' config
packages and never contacts the secret store, so it works offline and its
output can be committed and reused by run. Apps are the directories given as
arguments, or those listed in .ultra.toml when none are given.

Pass --compose-file to scope the output to one stack: gen then writes a service
block only for apps whose service the given compose file defines, so the result
merges cleanly onto a subset stack. Generate one file per stack this way.

```
ultra gen [app-path...] [flags]
```

### Options

```
      --compose-file string      scope the output to the services this compose file defines, relative to --root (default: every app)
      --config-dir string        config package directory under each app path (e.g. pkg/config) (default "config")
      --config-file string       path to the ultra config file (default .ultra.toml under --root)
  -h, --help                     help for gen
      --output-dir string        directory under --root the generated compose file is written to; point it at a committed path to keep it in version control (default "tmp")
      --output-filename string   file name of the generated compose file under --output-dir; set it to docker-compose.override.yml to have compose auto-load it (default "ultra.compose.yml")
      --root string              repo root the compose file and overrides are anchored to (default ".")
```

### SEE ALSO

* [ultra](ultra.md)	 - Wire every app's config.go secrets into local docker-compose dev

