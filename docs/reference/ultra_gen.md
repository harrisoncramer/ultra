## ultra gen

Generate the single compose file for the given apps without resolving secrets

### Synopsis

The gen command writes a single docker compose file that contains the bindings
for all ultra secrets defined in each app's config package. It does not contact
the secret provider; it merely sets the key/value pairs for the run command to
use.

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

