package resolve

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/spf13/pflag"
)

// ConfigResolverCommand registers a config resolver under a name selectable via
// --config-resolver. Setup binds the resolver's own flags on fs and returns a
// factory that builds the resolver from the repo root once those flags are
// parsed, mirroring SecretResolverCommand.
type ConfigResolverCommand struct {
	Name  string
	Short string
	Setup func(fs *pflag.FlagSet) func(root string) (ConfigResolver, error)
}

var configResolvers []ConfigResolverCommand

// RegisterConfigResolver adds a config resolver. The built-in resolvers register
// themselves; call this before Execute to add your own.
func RegisterConfigResolver(rc ConfigResolverCommand) {
	configResolvers = append(configResolvers, rc)
}

func init() {
	RegisterConfigResolver(ConfigResolverCommand{
		Name:  "docker-compose",
		Short: "Read non-secret config from a docker compose file",
		Setup: func(fs *pflag.FlagSet) func(root string) (ConfigResolver, error) {
			file := fs.String("compose-file", "docker-compose.yml", "docker compose file to read non-secret config from, relative to --root")
			return func(root string) (ConfigResolver, error) {
				return &dockerComposeConfig{composeFile: filepath.Join(root, *file)}, nil
			}
		},
	})
	RegisterConfigResolver(ConfigResolverCommand{
		Name:  "env",
		Short: "Use the process environment (non-secrets already set, e.g. in a container or pod)",
		Setup: func(_ *pflag.FlagSet) func(root string) (ConfigResolver, error) {
			return func(_ string) (ConfigResolver, error) {
				return envConfig{}, nil
			}
		},
	})
}

// FindConfigResolver returns the config resolver command registered under name.
func FindConfigResolver(name string) (ConfigResolverCommand, bool) {
	for _, rc := range configResolvers {
		if rc.Name == name {
			return rc, true
		}
	}
	return ConfigResolverCommand{}, false
}

// ConfigResolverNames lists the registered config resolver names for help text.
func ConfigResolverNames() string {
	names := make([]string, len(configResolvers))
	for i, rc := range configResolvers {
		names[i] = rc.Name
	}
	return strings.Join(names, ", ")
}

// layeredConfigResolver queries base then layers override on top, so an override
// value wins over the base for the same key. A nil override is a no-op.
type layeredConfigResolver struct {
	base     ConfigResolver
	override ConfigResolver
}

func (l layeredConfigResolver) Resolve(ctx context.Context, app string) (map[string]string, error) {
	out, err := l.base.Resolve(ctx, app)
	if err != nil {
		return nil, err
	}
	if l.override == nil {
		return out, nil
	}
	ov, err := l.override.Resolve(ctx, app)
	if err != nil {
		return nil, fmt.Errorf("config override resolver: %w", err)
	}
	if out == nil {
		out = make(map[string]string, len(ov))
	}
	for k, v := range ov {
		out[k] = v
	}
	return out, nil
}

// LayerConfigResolver wraps base so the override's values win, or returns base
// unchanged when no override is configured.
func LayerConfigResolver(base, override ConfigResolver) ConfigResolver {
	if override == nil {
		return base
	}
	return layeredConfigResolver{base: base, override: override}
}

// BuildConfigOverride builds the override config resolver named by name for root,
// or returns nil when name is empty. Its flags come from flags, bound on a
// private flag set.
func BuildConfigOverride(name string, flags map[string]string, root string) (ConfigResolver, error) {
	if name == "" {
		return nil, nil
	}
	rc, ok := FindConfigResolver(name)
	if !ok {
		return nil, fmt.Errorf("config-override resolver %q is not registered", name)
	}
	fs := pflag.NewFlagSet("config-override", pflag.ContinueOnError)
	build := rc.Setup(fs)
	applyFlagSet(fs, flags)
	return build(root)
}

// dockerComposeConfig reads apps' non-secret environment from a docker-compose
// file via `docker compose config`. The file is read once and cached across apps.
type dockerComposeConfig struct {
	composeFile string

	once     sync.Once
	services map[string]map[string]string
	err      error
}

func (d *dockerComposeConfig) Resolve(ctx context.Context, app string) (map[string]string, error) {
	d.once.Do(func() { d.services, d.err = d.load(ctx) })
	if d.err != nil {
		return nil, d.err
	}
	return d.services[app], nil
}

func (d *dockerComposeConfig) load(ctx context.Context) (map[string]map[string]string, error) {
	cmd := exec.CommandContext(ctx, "docker", "compose", "-f", d.composeFile, "config", "--format", "json")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("docker compose config: %s", msg)
	}

	var cfg struct {
		Services map[string]struct {
			Environment map[string]*string `json:"environment"`
		} `json:"services"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &cfg); err != nil {
		return nil, fmt.Errorf("parsing docker compose config: %w", err)
	}

	out := make(map[string]map[string]string, len(cfg.Services))
	for name, svc := range cfg.Services {
		env := make(map[string]string, len(svc.Environment))
		for k, v := range svc.Environment {
			if v != nil {
				env[k] = *v
			}
		}
		out[name] = env
	}
	return out, nil
}

// envConfig provides no non-secret values: the environment (a running container
// or pod) already holds them, and validate starts from the process env anyway.
type envConfig struct{}

func (envConfig) Resolve(_ context.Context, _ string) (map[string]string, error) {
	return map[string]string{}, nil
}
