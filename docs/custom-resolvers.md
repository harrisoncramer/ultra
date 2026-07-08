# Custom resolvers

## Writing a custom secret resolver

You don't fork ultra to add a backend. Import `github.com/harrisoncramer/ultra/cli`, register a secret resolver, and call `cli.Execute` from your own `main` function.

The built-in resolvers live in their own subpackages under `cli/resolvers` and register themselves when imported. The default `ultra` binary blank-imports all of them, but a custom `main` can import only the ones it needs, so a binary carries just those backends and their dependencies. Importing only 1password, for example, leaves the AWS SDK out of the build entirely.

```go
import (
	"github.com/harrisoncramer/ultra/cli"

	_ "github.com/harrisoncramer/ultra/cli/resolvers/onepassword"
)
```

If you'd like to suggest a new backend, please open a PR and I'll consider shipping it with the ultra core:

```go
package main

import (
	"context"
	"os"

	"github.com/harrisoncramer/ultra/cli"

	"github.com/spf13/pflag"
)

func main() {
	cli.RegisterSecretResolver(cli.SecretResolverCommand{
		Name:  "vault",
		Short: "Resolve secrets from HashiCorp Vault",
		Setup: func(fs *pflag.FlagSet) func(app string) cli.SecretResolver {
			var addr string
			fs.StringVar(&addr, "addr", "", "vault address")
			return func(app string) cli.SecretResolver {
				return vaultResolver{addr: addr, app: app}
			}
		},
	})
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}

// vaultResolver fetches secrets from wherever you keep them.
type vaultResolver struct {
	addr string
	app  string
}

func (v vaultResolver) Resolve(ctx context.Context, names []string) (map[string]string, error) {
	out := make(map[string]string, len(names))
	for _, name := range names {
		// Look up name for v.app and set out[name]; omit it if the store has no
		// such secret. Return a non-nil error only if the store is unreachable.
	}
	return out, nil
}
```

## Writing a custom config resolver

The default `docker-compose` resolver is what you want on your own machine. But `validate` is also useful as a pre-deploy gate: before shipping to staging or production, check that the app's `Config` still parses against the non-secret config that environment will actually give it. In Kubernetes, for instance, that config may live in a ConfigMap, where the custom resolver reads it.

Register one with `cli.RegisterConfigResolver` and select it with `--config-resolver`. Here it reads the `data` block of the ConfigMap that provides the app's env in a given environment:

```go
cli.RegisterConfigResolver(cli.ConfigResolverCommand{
	Name:  "configmap",
	Short: "Read non-secret config from a Kubernetes ConfigMap",
	Setup: func(fs *pflag.FlagSet) func(root string) (cli.ConfigResolver, error) {
		var env, dir string
		fs.StringVar(&env, "env", "", "environment whose ConfigMap to read")
		fs.StringVar(&dir, "manifests", "deploy", "directory holding <app>/<env>/configmap.yaml")
		return func(root string) (cli.ConfigResolver, error) {
			if env == "" {
				return nil, fmt.Errorf("configmap resolver requires an env")
			}
			return configMap{root: root, dir: dir, env: env}, nil
		}
	},
})

// configMap reads an app's non-secret env from the data block of the ConfigMap
// that provides it in the target environment.
type configMap struct{ root, dir, env string }

func (c configMap) Resolve(ctx context.Context, app string) (map[string]string, error) {
	path := filepath.Join(c.root, c.dir, app, c.env, "configmap.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading configmap for %s: %w", app, err)
	}
	var doc struct {
		Data map[string]string `yaml:"data"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return doc.Data, nil
}
```

Given a manifest like this, `Resolve` returns `LOG_LEVEL` and `DATABASE_HOST`, the values the platform sets alongside the secrets ultra resolves:

```yaml
# deploy/worker/staging/configmap.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: worker
data:
  LOG_LEVEL: info
  DATABASE_HOST: db.staging.internal
```

To configure multiple environments:

```toml
# .ultra.staging.toml
[config]
resolver = "configmap"

[config.configmap]
env = "staging"
```

```toml
# .ultra.production.toml
[config]
resolver = "configmap"

[config.configmap]
env = "production"
```

Then each environment is a configuration file:

```bash
ultra validate --config-file .ultra.staging.toml
ultra validate --config-file .ultra.production.toml
```
